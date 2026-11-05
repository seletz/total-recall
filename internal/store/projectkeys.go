package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ProjectKey is one raw project key of the archive, as the project
// dimension counts it: a git root, else the working directory of the
// sessions without a repository (Dir), with the remote URL of the
// repository, the number of top-level sessions (subagents count with
// their parent) and the time of the newest session.
type ProjectKey struct {
	Key      string
	Dir      bool
	Remote   string
	Sessions int
	Last     time.Time
}

// ProjectKeys lists the raw project keys of the archive by number of
// sessions, descending, then by key. Sessions without a working directory
// are left out; the configured projects are applied by the caller with
// project.Resolver.Key.
func (s *Store) ProjectKeys(ctx context.Context) ([]ProjectKey, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT COALESCE(NULLIF(s.git_root, ''), s.cwd) AS key,
			MIN(COALESCE(s.git_root, '') = ''), MAX(COALESCE(s.git_remote_url, '')), COUNT(*), MAX(COALESCE(s.ended_at, s.started_at))
		FROM sessions s
		LEFT JOIN sessions p ON p.id = s.parent_id
		WHERE p.id IS NULL AND COALESCE(NULLIF(s.git_root, ''), s.cwd, '') != ''
		GROUP BY key
		ORDER BY COUNT(*) DESC, key`)
	if err != nil {
		return nil, fmt.Errorf("store: project keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ProjectKey
	for rows.Next() {
		var k ProjectKey
		var last sql.NullString
		if err := rows.Scan(&k.Key, &k.Dir, &k.Remote, &k.Sessions, &last); err != nil {
			return nil, fmt.Errorf("store: project keys: %w", err)
		}
		k.Last = parseTime(last)
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: project keys: %w", err)
	}
	return out, nil
}
