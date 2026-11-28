package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// archiveVersion is the schema version that gives an archive its identity
// (archive_meta).
const archiveVersion = ArchiveVersion

// ArchiveVersion is the schema version from which an archive carries its
// identity (archive_meta, see Archive).
const ArchiveVersion = 11

// Archive is the identity of an archive in a pool of hosts: ID is random
// once per archive and stays the same in its copies and backups; Host is
// the name of the host that writes it (config key host). Both are empty
// before schema v11, Host also until a write opened with WithHost.
type Archive struct {
	ID   string `json:"id"`
	Host string `json:"host"`
}

// Archive returns the identity of the archive; the zero value on an
// archive before schema v11.
func (s *Store) Archive(ctx context.Context) (Archive, error) {
	var a Archive
	if s.Version() < archiveVersion {
		return a, nil
	}
	rows, err := s.db().QueryContext(ctx, "SELECT key, value FROM archive_meta WHERE key IN ('archive_id', 'host')")
	if err != nil {
		return a, fmt.Errorf("store: archive: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return a, fmt.Errorf("store: archive: %w", err)
		}
		switch k {
		case "archive_id":
			a.ID = v
		case "host":
			a.Host = v
		}
	}
	if err := rows.Err(); err != nil {
		return a, fmt.Errorf("store: archive: %w", err)
	}
	return a, nil
}

// recordHost stores name as the host of the archive unless it is already
// recorded; it only writes on a change, so the hooks that open the archive
// every few seconds do not take the write lock for it.
func (s *Store) recordHost(ctx context.Context, name string) error {
	var cur string
	err := s.db().QueryRowContext(ctx, "SELECT value FROM archive_meta WHERE key = 'host'").Scan(&cur)
	switch {
	case err == nil && cur == name:
		return nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("store: host: %w", err)
	}
	if _, err := s.db().ExecContext(ctx, "INSERT INTO archive_meta (key, value) VALUES ('host', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", name); err != nil {
		return fmt.Errorf("store: host: %w", err)
	}
	return nil
}
