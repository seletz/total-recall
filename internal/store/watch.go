package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// Change is what Poll found since the previous call.
type Change int

// Poll results.
const (
	// Unchanged: no other connection committed and the file is the same.
	Unchanged Change = iota
	// Modified: another connection (a hook, an ingest) committed to the
	// database since the previous Poll.
	Modified
	// Replaced: the file at the store's path is a different file than the
	// one open (maintenance swapped it); the store must be reopened with
	// Reopen before it reads current data.
	Replaced
)

// String names the change for logs and tests.
func (c Change) String() string {
	switch c {
	case Unchanged:
		return "unchanged"
	case Modified:
		return "modified"
	case Replaced:
		return "replaced"
	}
	return fmt.Sprintf("Change(%d)", int(c))
}

// db returns the open database.
func (s *Store) db() *sql.DB { return s.dbp.Load() }

// Poll reports whether the archive changed since the previous call: it is
// one PRAGMA data_version on a connection pinned for this purpose, which
// moves when any other connection commits, plus a stat of the file to
// notice a swap. No read transaction stays open between calls, so a
// maintenance run can checkpoint the WAL while a reader polls. The first
// call establishes the baseline and reports Unchanged. Poll is for one
// caller at a time; it is safe against concurrent queries and Reopen.
func (s *Store) Poll(ctx context.Context) (Change, error) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	fi, err := os.Stat(s.path)
	if err != nil {
		return Unchanged, fmt.Errorf("store: poll: %w", err)
	}
	if s.file != nil && !os.SameFile(s.file, fi) {
		return Replaced, nil
	}
	first := !s.watched
	v, err := s.dataVersion(ctx)
	if err != nil {
		return Unchanged, err
	}
	changed := !first && v != s.watchVersion
	s.watched, s.watchVersion = true, v
	if changed {
		// A commit may have been a migration: refresh the version so that
		// MigrationPending follows (best effort, the next Poll retries).
		if sv, err := schemaVersion(ctx, s.watch); err == nil {
			s.version = sv
		}
		return Modified, nil
	}
	return Unchanged, nil
}

// dataVersion reads PRAGMA data_version on the pinned connection, opening
// it when needed; a failed read drops the connection. The caller holds
// watchMu.
func (s *Store) dataVersion(ctx context.Context) (int64, error) {
	if s.watch == nil {
		c, err := s.db().Conn(ctx)
		if err != nil {
			return 0, fmt.Errorf("store: poll: %w", err)
		}
		s.watch, s.watched = c, false
	}
	var v int64
	if err := s.watch.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v); err != nil {
		_ = s.watch.Close()
		s.watch = nil
		return 0, fmt.Errorf("store: poll: %w", err)
	}
	return v, nil
}

// Reopen opens the file at the store's path again and closes the previous
// database: after a maintenance run replaced the archive, a long-running
// reader (the TUI) calls it when Poll reports Replaced. Queries that are
// running on the old database finish on it; new ones see the new file.
// The baseline of Poll is taken on the new file right away, before the
// caller reloads, so a commit in between is not missed. Only read-only
// stores reopen.
func (s *Store) Reopen() error {
	if !s.readOnly {
		return errors.New("store: reopen: store is not read-only")
	}
	db, v, fi, err := openReadOnly(s.path)
	if err != nil {
		return err
	}
	s.watchMu.Lock()
	if s.watch != nil {
		_ = s.watch.Close()
		s.watch = nil
	}
	s.watched = false
	s.version, s.file = v, fi
	old := s.dbp.Swap(db)
	if v, err := s.dataVersion(context.Background()); err == nil {
		s.watched, s.watchVersion = true, v
	}
	s.watchMu.Unlock()
	if old != nil {
		return old.Close()
	}
	return nil
}

// openReadOnly opens the database at path with mode=ro and PRAGMA
// query_only and returns it with its schema version and the file identity
// (for Poll). A schema newer than this binary is ErrSchemaTooNew.
func openReadOnly(path string) (*sql.DB, int, os.FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("store: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(path,
		"mode=ro",
		"_pragma=busy_timeout(10000)",
		"_pragma=query_only(1)",
	))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, 0, nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	v, err := schemaVersion(context.Background(), db)
	if err != nil {
		_ = db.Close()
		return nil, 0, nil, fmt.Errorf("store: open %s: schema version: %w", path, err)
	}
	if v > len(migrations) {
		_ = db.Close()
		return nil, 0, nil, fmt.Errorf("%w (database v%d, binary v%d)", ErrSchemaTooNew, v, len(migrations))
	}
	return db, v, fi, nil
}
