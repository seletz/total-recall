// Package store persists sessions and events in SQLite (modernc.org/sqlite,
// pure Go): schema and forward-only migrations, idempotent batch writes,
// queries for the CLI, read-only SQL and backups (see docs/design.md).
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
)

// Store is an open archive database.
type Store struct {
	db       *sql.DB
	loc      *time.Location
	now      func() time.Time
	readOnly bool
}

// Option configures Open.
type Option func(*Store)

// WithLocation sets the time zone used for events.ts_local (and thus the
// daily_activity view). Default Europe/Berlin.
func WithLocation(loc *time.Location) Option { return func(s *Store) { s.loc = loc } }

// WithClock sets the clock used for ingested_at. Default time.Now.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

func newStore(opts []Option) *Store {
	s := &Store{now: time.Now}
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
	db, err := sql.Open("sqlite", dsn(path,
		"_pragma=busy_timeout(10000)",
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_txlock=immediate",
	))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	s := newStore(opts)
	s.db = db
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// OpenReadOnly opens an existing database with mode=ro and PRAGMA query_only,
// so neither the API nor Query can modify it. No migrations are applied.
func OpenReadOnly(path string, opts ...Option) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(path,
		"mode=ro",
		"_pragma=busy_timeout(10000)",
		"_pragma=query_only(1)",
	))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	s := newStore(opts)
	s.db = db
	s.readOnly = true
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// SchemaVersion returns the applied schema version.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v)
	return v, err
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)"); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if v > len(migrations) {
		return fmt.Errorf("store: database schema version %d is newer than this binary supports (%d)", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", i+1, err)
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
	rows, err := s.db.QueryContext(ctx, q)
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
