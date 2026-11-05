package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// This file implements `total-recall migrate`: the only way an existing
// archive gets a newer schema. Open never migrates (see Open), so a hook
// cannot start a long migration unnoticed; the user backs up first and runs
// migrate when it suits. Migrate holds the maintenance lock exclusively
// (see maintenance.go) so no hook writes while the schema changes.

// MigrationInfo describes one schema migration for `total-recall migrate`.
type MigrationInfo struct {
	Version int    `json:"version"`
	Summary string `json:"summary"`
	// Space is the extra free space the migration needs while it runs, as
	// a factor of the database size (0: negligible, 1: about the size of
	// the database); see MigratePlan.NeedBytes.
	Space float64 `json:"space_factor"`
}

// PendingMigrations lists the migrations a database at version from still
// lacks, oldest first; empty when it is current.
func PendingMigrations(from int) []MigrationInfo {
	out := []MigrationInfo{}
	for v := from + 1; v <= len(migrations); v++ {
		n := migrationNotes[v-1]
		out = append(out, MigrationInfo{Version: v, Summary: n.Summary, Space: n.Space})
	}
	return out
}

// Progress is one progress report of a running migration: Version is the
// migration running; Done and Total count its work units (rows), both 0
// when the migration starts or does not count.
type Progress struct {
	Version     int
	Done, Total int64
}

// MigratePlan is what Migrate would do to the database at DB.
type MigratePlan struct {
	DB string `json:"db"`
	// From is the version of the database, To the version of the binary.
	From int `json:"from"`
	To   int `json:"to"`
	// Steps are the pending migrations, oldest first.
	Steps []MigrationInfo `json:"steps"`
	// SizeBytes is the size of the database file plus its WAL.
	SizeBytes int64 `json:"size_bytes"`
	// Backup is the pre-migration copy Migrate writes ("" when nothing is
	// pending), NeedBytes the free space it needs next to the database:
	// the copy, the WAL growth of the largest step, a quarter of headroom.
	Backup    string `json:"backup"`
	NeedBytes int64  `json:"need_bytes"`
}

// Pending reports whether the plan has migrations to apply.
func (p MigratePlan) Pending() bool { return p.From < p.To }

// PlanMigration reads the version of the database at path (read-only) and
// returns what Migrate would do. A missing database is an error, a newer
// schema ErrSchemaTooNew.
func PlanMigration(ctx context.Context, path string) (MigratePlan, error) {
	st, err := OpenReadOnly(path)
	if err != nil {
		return MigratePlan{}, err
	}
	defer func() { _ = st.Close() }()
	v, err := st.SchemaVersion(ctx)
	if err != nil {
		return MigratePlan{}, fmt.Errorf("store: migrate: %w", err)
	}
	return planFor(path, v), nil
}

// planFor builds the plan for a database at version v.
func planFor(path string, v int) MigratePlan {
	plan := MigratePlan{DB: path, From: v, To: len(migrations), Steps: PendingMigrations(v), SizeBytes: fileSize(path)}
	if !plan.Pending() {
		return plan
	}
	plan.Backup = PreMigrationBackup(path, v)
	var space float64
	for _, s := range plan.Steps {
		space = max(space, s.Space)
	}
	size := plan.SizeBytes
	plan.NeedBytes = size + int64(float64(size)*space) + size/4
	return plan
}

// MigrateOptions configure Migrate.
type MigrateOptions struct {
	// LockDir holds the maintenance lock (the ingest lock dir); required.
	LockDir string
	// Wait is how long to wait for writers to release the lock.
	Wait time.Duration
	// Logf receives one line per step; default discard.
	Logf func(format string, args ...any)
	// Progress receives the progress of the running migration; default none.
	Progress func(Progress)
	// RawTrimmers are the harnesses' raw trimmers (WithRawTrimmers) for
	// migration 5; raw lines of a harness without one are only compressed.
	RawTrimmers map[string]RawTrimFunc
}

// MigrateResult reports what Migrate did. Plan is the plan it executed;
// nothing was done when the plan had nothing pending.
type MigrateResult struct {
	Plan     MigratePlan
	Duration time.Duration
}

// Migrate brings the database at path to the schema of this binary: plan,
// free-space check (ErrDiskSpace), exclusive maintenance lock (so no hook
// writes meanwhile; ErrWritersActive when writers hold the archive beyond
// o.Wait, ErrMaintenance while another run holds it), the pre-migration
// copy PreMigrationBackup, then every pending migration in its own
// transaction. A failure leaves the database at the last version that
// completed; the copy stays. A current database is left alone.
func Migrate(ctx context.Context, path string, o MigrateOptions) (MigrateResult, error) {
	start := time.Now()
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	plan, err := PlanMigration(ctx, path)
	res := MigrateResult{Plan: plan}
	if err != nil {
		return res, err
	}
	if !plan.Pending() {
		return res, nil
	}
	free, err := freeSpace(filepath.Dir(path))
	if err != nil {
		return res, fmt.Errorf("store: migrate: %w", err)
	}
	if free < plan.NeedBytes {
		return res, fmt.Errorf("%w next to the database: %s free, about %s needed for the copy and the migration", ErrDiskSpace, mib(free), mib(plan.NeedBytes))
	}
	if o.LockDir == "" {
		return res, errors.New("store: migrate: no lock dir")
	}
	release, err := lockMaintenance(o.LockDir, o.Wait)
	if err != nil {
		return res, err
	}
	defer release()
	st, err := Open(path, AllowOutdated(), WithLogf(o.Logf), WithProgress(o.Progress), WithRawTrimmers(o.RawTrimmers))
	if err != nil {
		return res, err
	}
	defer func() { _ = st.Close() }()
	// Another process may have migrated between the plan and the lock;
	// migrateTo reads the version again and does only what is left.
	if err := st.migrateTo(ctx, len(migrations)); err != nil {
		return res, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

// CreateAtVersion creates an empty database at path with the historical
// schema version v (1 to LatestSchemaVersion). It exists for tests and tools
// that need an outdated archive; path must not exist yet.
func CreateAtVersion(path string, v int) error {
	if v < 1 || v > len(migrations) {
		return fmt.Errorf("store: create at version %d: versions are 1 to %d", v, len(migrations))
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("store: create at version: %s exists", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("store: create at version: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store: create at version: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(path, "_pragma=journal_mode(WAL)"))
	if err != nil {
		return fmt.Errorf("store: create at version: %w", err)
	}
	defer func() { _ = db.Close() }()
	s := newStore(nil)
	s.dbp.Store(db)
	s.path = path
	return s.migrateTo(context.Background(), v)
}
