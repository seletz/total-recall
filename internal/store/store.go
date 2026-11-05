// Package store persists sessions and events in SQLite (modernc.org/sqlite,
// pure Go): schema and forward-only migrations, idempotent batch writes,
// queries for the CLI, read-only SQL and backups (see docs/explanation/design.md).
//
// All timestamps are stored as UTC ISO 8601 text with millisecond precision
// ("2026-10-31T22:21:58.123Z"), which sorts lexicographically.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata" // static builds must not depend on the system zoneinfo

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// TimeFormat is the storage format of all timestamps (always UTC).
const TimeFormat = "2006-01-02T15:04:05.000Z"

// localTimeFormat is the format of events.ts_local.
const localTimeFormat = "2006-01-02T15:04:05.000-07:00"

// Errors returned by lookups.
var (
	ErrNotFound  = errors.New("store: not found")
	ErrAmbiguous = errors.New("store: ambiguous reference")
	// ErrSchemaTooNew means the database was migrated by a newer binary.
	ErrSchemaTooNew = errors.New("store: database schema is newer than this binary supports")
	// ErrSchemaOutdated means the database has an older schema than this
	// binary and the caller did not allow that: nothing is migrated
	// implicitly, the user runs `total-recall migrate` (see Migrate).
	ErrSchemaOutdated = errors.New("store: database schema is older than this binary; run `total-recall backup`, then `total-recall migrate`")
)

// LatestSchemaVersion is the schema version this binary migrates to.
func LatestSchemaVersion() int { return len(migrations) }

// Store is an open archive database.
type Store struct {
	// dbp is the open database. Read-only stores swap it in Reopen while
	// queries may be running, hence the atomic pointer; db() reads it.
	dbp      atomic.Pointer[sql.DB]
	path     string
	loc      *time.Location
	now      func() time.Time
	logf     func(format string, args ...any)
	readOnly bool
	// version is the schema version of the open database; Poll and Reopen
	// refresh it under watchMu, which also guards the readers.
	version int
	// allowOutdated lets Open accept an older schema without migrating it.
	allowOutdated bool
	// progress receives the progress of migrations (WithProgress).
	progress func(Progress)
	lockDir  string // WithLockDir: shared maintenance lock held until Close
	unlock   func()
	// Poll state (read-only stores): the file identity at open, the pinned
	// connection with the data_version it last saw. watchMu also orders
	// Reopen against Poll.
	watchMu      sync.Mutex
	file         os.FileInfo
	watch        *sql.Conn
	watched      bool
	watchVersion int64
}

// Option configures Open.
type Option func(*Store)

// WithLocation sets the time zone used for events.ts_local (and thus the
// daily_activity view). Default Europe/Berlin.
func WithLocation(loc *time.Location) Option { return func(s *Store) { s.loc = loc } }

// WithLogf sets a logger for notable events such as pre-migration backups.
// Default: discard.
func WithLogf(f func(format string, args ...any)) Option { return func(s *Store) { s.logf = f } }

// WithClock sets the clock used for ingested_at. Default time.Now.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// AllowOutdated makes Open accept a database with an older schema than this
// binary without migrating it, instead of failing with ErrSchemaOutdated.
// For backup (the step before a migration) and Migrate itself; a store
// opened this way must not rely on columns of newer migrations.
func AllowOutdated() Option { return func(s *Store) { s.allowOutdated = true } }

// WithProgress sets the receiver of migration progress (see Progress);
// Migrate passes its option through. Default: none.
func WithProgress(f func(Progress)) Option { return func(s *Store) { s.progress = f } }

func newStore(opts []Option) *Store {
	s := &Store{now: time.Now, logf: func(string, ...any) {}}
	for _, o := range opts {
		o(s)
	}
	if s.loc == nil {
		loc, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			loc = time.UTC
		}
		s.loc = loc
	}
	return s
}

// dsn builds a SQLite URI for path. Characters with meaning in URIs are escaped.
func dsn(path string, params ...string) string {
	esc := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	return "file:" + esc + "?" + strings.Join(params, "&")
}

// Open opens (creating if needed) the database at path read-write and
// enables WAL, foreign keys and a 10 s busy timeout. A new, empty database
// gets the latest schema. An existing database is never migrated here: an
// older schema is ErrSchemaOutdated unless AllowOutdated is given, a newer
// one ErrSchemaTooNew. Migrate applies pending migrations.
func Open(path string, opts ...Option) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	s := newStore(opts)
	if s.lockDir != "" {
		unlock, err := lockShared(s.lockDir)
		if err != nil {
			return nil, err
		}
		s.unlock = unlock
	}
	db, err := sql.Open("sqlite", dsn(path,
		"_pragma=busy_timeout(10000)",
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_txlock=immediate",
	))
	if err != nil {
		s.release()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	s.dbp.Store(db)
	s.path = path
	v, err := s.openSchema(context.Background())
	if err != nil {
		_ = db.Close()
		s.release()
		return nil, err
	}
	s.version = v
	return s, nil
}

// openSchema prepares the schema of a database opened read-write and
// returns its version afterwards: a new, empty database gets the latest
// schema, an existing one is only checked (see Open).
func (s *Store) openSchema(ctx context.Context) (int, error) {
	v, err := s.ensureVersion(ctx)
	if err != nil {
		return 0, err
	}
	latest := len(migrations)
	switch {
	case v == 0:
		if err := s.migrateTo(ctx, latest); err != nil {
			return 0, err
		}
		return latest, nil
	case v < latest && !s.allowOutdated:
		return 0, fmt.Errorf("%w (database v%d, binary v%d)", ErrSchemaOutdated, v, latest)
	}
	return v, nil
}

// OpenReadOnly opens an existing database with mode=ro and PRAGMA query_only,
// so neither the API nor Query can modify it. No migrations are applied.
func OpenReadOnly(path string, opts ...Option) (*Store, error) {
	db, v, fi, err := openReadOnly(path)
	if err != nil {
		return nil, err
	}
	s := newStore(opts)
	s.dbp.Store(db)
	s.path = path
	s.readOnly = true
	s.version = v
	s.file = fi
	return s, nil
}

// MigrationPending reports whether the database has an older schema than
// this binary, so `total-recall migrate` is due. Queries may miss newer
// columns until then.
func (s *Store) MigrationPending() bool {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	return s.version < len(migrations)
}

// Version is the schema version of the open database as found at open and
// refreshed by Poll and Reopen; SchemaVersion queries it.
func (s *Store) Version() int {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	return s.version
}

// SchemaWarning returns a hint when a migration is pending (see
// MigrationPending), else "".
func (s *Store) SchemaWarning() string {
	s.watchMu.Lock()
	v := s.version
	s.watchMu.Unlock()
	if v >= len(migrations) {
		return ""
	}
	return fmt.Sprintf("database schema v%d is older than this binary (v%d); run `total-recall backup`, then `total-recall migrate`",
		v, len(migrations))
}

// Close closes the database and releases the maintenance lock, if any.
func (s *Store) Close() error {
	s.watchMu.Lock()
	if s.watch != nil {
		_ = s.watch.Close()
		s.watch = nil
	}
	s.watchMu.Unlock()
	err := s.db().Close()
	s.release()
	return err
}

// release gives up the shared maintenance lock (once).
func (s *Store) release() {
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
}

// SchemaVersion returns the applied schema version.
// A database without a schema_version table has version 0.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	return schemaVersion(ctx, s.db())
}

// rowQuerier is what schemaVersion needs: *sql.DB or *sql.Conn.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// schemaVersion is SchemaVersion on a bare database or connection.
func schemaVersion(ctx context.Context, db rowQuerier) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'").Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	var v int
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v)
	return v, err
}

// ensureVersion creates the schema_version table if needed and returns the
// version; a version above the binary's is ErrSchemaTooNew.
func (s *Store) ensureVersion(ctx context.Context) (int, error) {
	if _, err := s.db().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)"); err != nil {
		return 0, fmt.Errorf("store: migrate: %w", err)
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: migrate: %w", err)
	}
	if v > len(migrations) {
		return 0, fmt.Errorf("%w (database v%d, binary v%d)", ErrSchemaTooNew, v, len(migrations))
	}
	return v, nil
}

// preMigrationBackup writes a VACUUM INTO copy of a non-empty database at
// version v to PreMigrationBackup(path, v) before pending migrations are
// applied.
func (s *Store) preMigrationBackup(ctx context.Context, v int) error {
	if v == 0 || s.path == "" {
		return nil
	}
	dst := PreMigrationBackup(s.path, v)
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: pre-migration backup: %w", err)
	}
	if _, err := s.db().ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("store: pre-migration backup: %w", err)
	}
	s.logf("store: schema v%d -> v%d: backup written to %s", v, len(migrations), dst)
	return nil
}

// PreMigrationBackup is the path of the copy Migrate writes next to the
// database at path before it migrates from version v: "<db>.pre-v<v>.bak".
func PreMigrationBackup(path string, v int) string {
	return fmt.Sprintf("%s.pre-v%d.bak", path, v)
}

// migrateTo applies migrations up to version target (forward-only), each in
// its own transaction, after a pre-migration backup of a non-empty database.
func (s *Store) migrateTo(ctx context.Context, target int) error {
	v, err := s.ensureVersion(ctx)
	if err != nil {
		return err
	}
	if v < target {
		if err := s.preMigrationBackup(ctx, v); err != nil {
			return err
		}
	}
	for i := v; i < target; i++ {
		if err := s.applyMigration(ctx, i+1); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs migration version: its SQL block and its Go step, if
// any, in one transaction with the new schema_version, then a WAL
// checkpoint so that the WAL of a large migration does not stay that large.
func (s *Store) applyMigration(ctx context.Context, version int) error {
	start := time.Now()
	s.report(Progress{Version: version})
	fail := func(tx *sql.Tx, err error) error {
		_ = tx.Rollback()
		return fmt.Errorf("store: migration %d: %w", version, err)
	}
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if _, err := tx.ExecContext(ctx, migrations[version-1]); err != nil {
		return fail(tx, err)
	}
	if step, ok := migrationSteps[version]; ok {
		report := func(done, total int64) { s.report(Progress{Version: version, Done: done, Total: total}) }
		if err := step(ctx, tx, report); err != nil {
			return fail(tx, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM schema_version"); err != nil {
		return fail(tx, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_version(version) VALUES (?)", version); err != nil {
		return fail(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: migration %d: %w", version, err)
	}
	// Best effort: readers may keep the WAL busy, the next checkpoint
	// catches up.
	var busy, frames, done int
	_ = s.db().QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &done)
	s.logf("store: migration v%d applied (%s)", version, time.Since(start).Round(time.Millisecond))
	return nil
}

// report passes p to the progress receiver, if any.
func (s *Store) report(p Progress) {
	if s.progress != nil {
		s.progress(p)
	}
}

// formatTime returns the storage form of t, or NULL for the zero time.
func formatTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(TimeFormat)
}

// parseTime parses a stored timestamp; NULL/empty gives the zero time.
func parseTime(ns sql.NullString) time.Time {
	if !ns.Valid || ns.String == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// nullStr maps "" to NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullInt maps 0 to NULL.
func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// Query runs an arbitrary SQL statement and returns column names and rows.
// []byte values are returned as strings. Use it on a store opened with
// OpenReadOnly for untrusted input: it does not restrict statements itself.
func (s *Store) Query(ctx context.Context, q string) ([]string, [][]any, error) {
	rows, err := s.db().QueryContext(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}
