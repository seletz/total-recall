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
	version  int    // schema version found at open (read-only stores)
	lockDir  string // WithLockDir: shared maintenance lock held until Close
	unlock   func()
	// rawTrimmers reduce a stored raw line of a harness to the fields the
	// harness needs again (migration 5); key = harness name.
	rawTrimmers map[string]RawTrimFunc
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

// RawTrimFunc reduces a raw transcript line to the fields its harness needs
// to read it again (harness.RawTrimmer). An error keeps the line as it is.
type RawTrimFunc func(raw []byte) ([]byte, error)

// WithRawTrimmers sets the trimmers migration 5 applies to the stored raw
// lines, by harness name. Lines of a harness without a trimmer are only
// compressed.
func WithRawTrimmers(m map[string]RawTrimFunc) Option {
	return func(s *Store) { s.rawTrimmers = m }
}

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

// Open opens (creating if needed) the database at path read-write, enables
// WAL, foreign keys and a 10 s busy timeout, and applies pending migrations.
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
	if err := s.migrateTo(context.Background(), len(migrations)); err != nil {
		_ = db.Close()
		s.release()
		return nil, err
	}
	return s, nil
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

// SchemaWarning returns a hint for a read-only store whose database has an
// older schema than this binary (queries may miss newer columns), else "".
func (s *Store) SchemaWarning() string {
	if !s.readOnly || s.version >= len(migrations) {
		return ""
	}
	return fmt.Sprintf("database schema v%d is older than this binary (v%d); run a write command such as `total-recall ingest --all` to migrate",
		s.version, len(migrations))
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

// schemaVersion is SchemaVersion on a bare database.
func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
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

// preMigrationBackup writes a VACUUM INTO copy of a non-empty database at
// version v to "<db>.pre-v<v>.bak" before pending migrations are applied.
func (s *Store) preMigrationBackup(ctx context.Context, v int) error {
	if v == 0 || s.path == "" {
		return nil
	}
	dst := fmt.Sprintf("%s.pre-v%d.bak", s.path, v)
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: pre-migration backup: %w", err)
	}
	if _, err := s.db().ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("store: pre-migration backup: %w", err)
	}
	s.logf("store: schema v%d -> v%d: backup written to %s", v, len(migrations), dst)
	return nil
}

// migrateTo applies migrations up to version target (forward-only).
func (s *Store) migrateTo(ctx context.Context, target int) error {
	if _, err := s.db().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)"); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if v > len(migrations) {
		return fmt.Errorf("%w (database v%d, binary v%d)", ErrSchemaTooNew, v, len(migrations))
	}
	if v < target {
		if err := s.preMigrationBackup(ctx, v); err != nil {
			return err
		}
	}
	steps := s.migrationSteps()
	for i := v; i < target; i++ {
		tx, err := s.db().BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		if step := steps[i+1]; step != nil {
			if err := step(ctx, tx, s.progressLogger(i+1)); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("store: migration %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM schema_version"); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_version(version) VALUES (?)", i+1); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
	}
	return nil
}

// progressLogger returns a report function for a migration step that logs
// the progress at most every two seconds and at the end.
func (s *Store) progressLogger(version int) func(done, total int64) {
	var last time.Time
	return func(done, total int64) {
		now := time.Now()
		if done < total && now.Sub(last) < 2*time.Second {
			return
		}
		last = now
		s.logf("store: migration %d: %d/%d rows rewritten", version, done, total)
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
