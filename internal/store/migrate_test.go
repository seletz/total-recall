package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openAtVersion creates a database at path with only migrations 1..k applied.
func openAtVersion(t *testing.T, path string, k int) {
	t.Helper()
	db, err := sql.Open("sqlite", dsn(path, "_pragma=journal_mode(WAL)"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s := newStore(nil)
	s.dbp.Store(db)
	s.path = path
	if err := s.migrateTo(ctx, k); err != nil {
		t.Fatalf("migrate to %d: %v", k, err)
	}
}

// sampleRows inserts rows using only columns that exist since version 1, so
// they are valid at every historical version.
var sampleRows = []string{
	`INSERT INTO sources (path, harness, size, mtime, "offset", ingested_at)
		VALUES ('/p/a.jsonl', 'claude', 100, '2026-10-01T10:00:00.000Z', 90, '2026-10-01T10:00:01.000Z')`,
	`INSERT INTO sessions (id, harness, native_id, name, agent_name, cwd, git_remote_url, started_at, ended_at)
		VALUES ('claude:s1', 'claude', 's1', 'Title', 'agent', '/w', 'https://github.com/o/r',
			'2026-10-01T10:00:00.000Z', '2026-10-01T11:00:00.000Z')`,
	// s0 is the session s1 continues (migration 4 adds the link columns;
	// the test sets them after migrating).
	`INSERT INTO sessions (id, harness, native_id, name, cwd, started_at, ended_at)
		VALUES ('claude:s0', 'claude', 's0', 'Earlier', '/w', '2026-09-30T10:00:00.000Z', '2026-09-30T11:00:00.000Z')`,
	`INSERT INTO events (id, session_id, ts, kind, role, actor, text, source_path, source_offset, raw)
		VALUES ('claude:e1', 'claude:s1', '2026-10-01T10:00:00.000Z', 'message', 'user', 'human',
			'hello migration world', '/p/a.jsonl', 0, '{}')`,
	`INSERT INTO dirs (cwd, git_root, git_remote_url, resolved_at)
		VALUES ('/w', '/w', 'https://github.com/o/r', '2026-10-01T10:00:00.000Z')`,
}

// TestMigrateFromEveryVersion builds a database at every historical schema
// version, fills it, migrates to the latest version and checks that the data
// survived and a pre-migration backup was written. New migrations are
// covered automatically.
func TestMigrateFromEveryVersion(t *testing.T) {
	for k := 1; k <= LatestSchemaVersion(); k++ {
		t.Run(fmt.Sprintf("from v%d", k), func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "archive.db")
			openAtVersion(t, p, k)
			db, err := sql.Open("sqlite", dsn(p))
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range sampleRows {
				if _, err := db.ExecContext(ctx, q); err != nil {
					t.Fatalf("v%d: %s: %v", k, q, err)
				}
			}
			_ = db.Close()

			var logged []string
			s, err := Open(p, WithLogf(func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() { _ = s.Close() }()
			if v, _ := s.SchemaVersion(ctx); v != LatestSchemaVersion() {
				t.Errorf("version = %d, want %d", v, LatestSchemaVersion())
			}
			checks := map[string]int{
				"SELECT count(*) FROM sources WHERE \"offset\" = 90":                        1,
				"SELECT count(*) FROM sessions WHERE name = 'Title' AND cwd = '/w'":         1,
				"SELECT count(*) FROM events WHERE text = 'hello migration world'":          1,
				"SELECT count(*) FROM events_fts WHERE events_fts MATCH 'migration'":        1,
				"SELECT count(*) FROM dirs WHERE git_root = '/w'":                           1,
				"SELECT count(*) FROM events WHERE raw_truncated = 0 AND raw = '{}'":        1,
				"SELECT count(*) FROM exchanges WHERE request = 'hello migration world'":    1,
				"SELECT count(*) FROM daily_activity WHERE repo = 'https://github.com/o/r'": 1,
			}
			for q, want := range checks {
				if got := countRows(t, s, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
			if _, err := s.Session(ctx, "claude:s1"); err != nil {
				t.Errorf("Session after migration: %v", err)
			}
			// Migration 4: the link and account columns are usable and indexed,
			// and the reverse link is derived from them.
			if _, err := s.db().ExecContext(ctx, `UPDATE sessions SET continued_in = 'claude:s1', account_id = 'acc-1',
				organization_id = 'org-1' WHERE id = 'claude:s0'`); err != nil {
				t.Fatalf("set v4 columns: %v", err)
			}
			v4 := map[string]int{
				"SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('sessions_continued_in', 'sessions_account_id')": 2,
				"SELECT count(*) FROM sessions WHERE continued_in IS NULL AND account_id IS NULL AND organization_id IS NULL":          1,
			}
			for q, want := range v4 {
				if got := countRows(t, s, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
			if si, err := s.Session(ctx, "claude:s1"); err != nil || si.ContinuedFrom != "claude:s0" {
				t.Errorf("s1 after migration: continued_from %q (%v), want claude:s0", si.ContinuedFrom, err)
			}
			if si, err := s.Session(ctx, "claude:s0"); err != nil || si.ContinuedIn != "claude:s1" || si.AccountID != "acc-1" || si.OrganizationID != "org-1" {
				t.Errorf("s0 after migration: %+v (%v)", si.Session, err)
			}
			if list, err := s.Sessions(ctx, SessionFilter{Account: "acc"}); err != nil || ids(list) != "claude:s0" {
				t.Errorf("Sessions by account after migration = %q (%v)", ids(list), err)
			}

			bak := fmt.Sprintf("%s.pre-v%d.bak", p, k)
			_, statErr := os.Stat(bak)
			if k < LatestSchemaVersion() {
				if statErr != nil {
					t.Errorf("no pre-migration backup %s: %v", bak, statErr)
				}
				if len(logged) == 0 || !strings.Contains(strings.Join(logged, "\n"), bak) {
					t.Errorf("backup not logged: %q", logged)
				}
			} else if statErr == nil {
				t.Errorf("unexpected backup %s for up-to-date DB", bak)
			}
		})
	}
}

func TestNoBackupForFreshDB(t *testing.T) {
	_, p := openTestStore(t)
	matches, _ := filepath.Glob(p + ".pre-v*.bak")
	if len(matches) != 0 {
		t.Errorf("fresh DB got backups: %v", matches)
	}
}

func TestOpenReadOnlySchemaVersion(t *testing.T) {
	t.Run("newer is an error", func(t *testing.T) {
		s, p := openTestStore(t)
		if _, err := s.db().ExecContext(ctx, "UPDATE schema_version SET version = 999"); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
		_, err := OpenReadOnly(p)
		if !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("OpenReadOnly = %v, want ErrSchemaTooNew", err)
		}
		if _, err := Open(p); !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("Open = %v, want ErrSchemaTooNew", err)
		}
	})
	t.Run("older works and says how to migrate", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "archive.db")
		openAtVersion(t, p, 1)
		s, err := OpenReadOnly(p)
		if err != nil {
			t.Fatalf("OpenReadOnly: %v", err)
		}
		defer func() { _ = s.Close() }()
		if LatestSchemaVersion() > 1 {
			w := s.SchemaWarning()
			if !strings.Contains(w, "older") || !strings.Contains(w, "ingest") {
				t.Errorf("SchemaWarning = %q", w)
			}
		}
		if _, _, err := s.Query(ctx, "SELECT count(*) FROM sessions"); err != nil {
			t.Errorf("query on older DB: %v", err)
		}
		if v, _ := s.SchemaVersion(ctx); v != 1 {
			t.Errorf("read-only open migrated the DB: version %d", v)
		}
	})
	t.Run("current has no warning", func(t *testing.T) {
		_, p := openTestStore(t)
		s, err := OpenReadOnly(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		if w := s.SchemaWarning(); w != "" {
			t.Errorf("SchemaWarning = %q", w)
		}
	})
}
