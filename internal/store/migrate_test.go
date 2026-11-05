package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store/rawz"
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

// sampleRaw is the raw line of event e1 as an archive before v5 stored it:
// a user record with Claude Code's bookkeeping. Migration 5 trims it with
// the harness's trimmer (here fakeTrim) and compresses it.
const sampleRaw = `{"type":"user","uuid":"e1","sessionId":"s1","cwd":"/w","junk":{"big":"bookkeeping"},"message":{"role":"user","content":"hello migration world"}}`

// sampleRawTrimmed is what fakeTrim makes of sampleRaw.
const sampleRawTrimmed = `{"message":{"role":"user","content":"hello migration world"},"type":"user","uuid":"e1"}`

// fakeTrim stands in for a harness's RawTrimFunc: it keeps type, uuid and
// message and drops the rest.
func fakeTrim(raw []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k := range m {
		if k != "type" && k != "uuid" && k != "message" {
			delete(m, k)
		}
	}
	return json.Marshal(m)
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
	// A session of another harness: no trimmer, its raw is only compressed.
	`INSERT INTO sessions (id, harness, native_id, cwd, started_at, ended_at)
		VALUES ('other:x1', 'other', 'x1', '/w', '2026-10-02T10:00:00.000Z', '2026-10-02T11:00:00.000Z')`,
	`INSERT INTO dirs (cwd, git_root, git_remote_url, resolved_at)
		VALUES ('/w', '/w', 'https://github.com/o/r', '2026-10-01T10:00:00.000Z')`,
}

// sampleEvents are the event rows of sampleRows, in the columns of a
// version before 5 (source_path, raw as text) and of version 5 or later
// (source_id, raw as a compressed blob). e1: the raw line above, source
// known. e2: a source that has no sources row (migration 5 creates one).
// e3: raw dropped for size. x1: another harness, raw kept as it is.
func sampleEvents(version int) []struct {
	q    string
	args []any
} {
	type r = struct {
		q    string
		args []any
	}
	if version < 5 {
		q := `INSERT INTO events (id, session_id, ts, kind, role, actor, text, source_path, source_offset, raw) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		return []r{
			{q, []any{"claude:e1", "claude:s1", "2026-10-01T10:00:00.000Z", "message", "user", "human", "hello migration world", "/p/a.jsonl", 0, sampleRaw}},
			{q, []any{"claude:e2", "claude:s1", "2026-10-01T10:01:00.000Z", "message", "assistant", "agent", "reply", "/p/gone.jsonl", 10, `{"type":"assistant","noise":1}`}},
			{q, []any{"claude:e3", "claude:s1", "2026-10-01T10:02:00.000Z", "tool_result", "tool", "tool", "big result", "/p/a.jsonl", 20, nil}},
			{q, []any{"other:x1", "other:x1", "2026-10-02T10:00:00.000Z", "message", "user", "human", "foreign", "/q/x.log", 0, `{"foreign":"record","junk":1}`}},
		}
	}
	q := `INSERT INTO events (id, session_id, ts, kind, role, actor, text, source_id, source_offset, raw) VALUES (?, ?, ?, ?, ?, ?, ?, (SELECT id FROM sources WHERE path = ?), ?, ?)`
	return []r{
		{`INSERT INTO sources (path, harness) VALUES ('/p/gone.jsonl', 'claude'), ('/q/x.log', 'other')`, nil},
		{q, []any{"claude:e1", "claude:s1", "2026-10-01T10:00:00.000Z", "message", "user", "human", "hello migration world", "/p/a.jsonl", 0, rawz.Encode(sampleRawTrimmed)}},
		{q, []any{"claude:e2", "claude:s1", "2026-10-01T10:01:00.000Z", "message", "assistant", "agent", "reply", "/p/gone.jsonl", 10, rawz.Encode(`{"type":"assistant"}`)}},
		{q, []any{"claude:e3", "claude:s1", "2026-10-01T10:02:00.000Z", "tool_result", "tool", "tool", "big result", "/p/a.jsonl", 20, nil}},
		{q, []any{"other:x1", "other:x1", "2026-10-02T10:00:00.000Z", "message", "user", "human", "foreign", "/q/x.log", 0, rawz.Encode(`{"foreign":"record","junk":1}`)}},
	}
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
			for _, e := range sampleEvents(k) {
				if _, err := db.ExecContext(ctx, e.q, e.args...); err != nil {
					t.Fatalf("v%d: %s: %v", k, e.q, err)
				}
			}
			if k >= 3 { // raw_truncated arrives with v3
				if _, err := db.ExecContext(ctx, "UPDATE events SET raw_truncated = 1 WHERE id = 'claude:e3'"); err != nil {
					t.Fatal(err)
				}
			}
			_ = db.Close()

			var logged []string
			s, err := Open(p, WithLogf(func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }),
				WithRawTrimmers(map[string]RawTrimFunc{"claude": fakeTrim}))
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
				"SELECT count(*) FROM events_fts WHERE events_fts MATCH 'foreign'":          1,
				"SELECT count(*) FROM dirs WHERE git_root = '/w'":                           1,
				"SELECT count(*) FROM exchanges WHERE request = 'hello migration world'":    1,
				"SELECT count(*) FROM daily_activity WHERE repo = 'https://github.com/o/r'": 1,
				// Migration 5: raw trimmed (claude) or kept (other), compressed, readable through raw_text.
				"SELECT count(*) FROM events WHERE id = 'claude:e1' AND typeof(raw) = 'blob' AND raw_text(raw) = '" + sampleRawTrimmed + "'":           1,
				"SELECT count(*) FROM events WHERE id = 'other:x1' AND typeof(raw) = 'blob' AND raw_text(raw) = '{\"foreign\":\"record\",\"junk\":1}'": 1,
				"SELECT count(*) FROM events WHERE id = 'claude:e3' AND raw IS NULL AND raw_truncated = " + fmt.Sprint(max(min(k, 3)-2, 0)):            1,
				"SELECT count(*) FROM events WHERE raw IS NOT NULL AND typeof(raw) <> 'blob'":                                                          0,
				// source_path became source_id; the unknown source got a bare row.
				"SELECT count(*) FROM sources": 3,
				"SELECT count(*) FROM sources WHERE path = '/p/gone.jsonl' AND harness = 'claude' AND \"offset\" = 0":                      1,
				"SELECT count(*) FROM sources WHERE path = '/q/x.log' AND harness = 'other'":                                               1,
				"SELECT count(*) FROM events e JOIN sources s ON s.id = e.source_id WHERE e.id = 'claude:e2' AND s.path = '/p/gone.jsonl'": 1,
				"SELECT count(*) FROM events WHERE source_id IS NULL":                                                                      0,
				"SELECT count(*) FROM pragma_table_info('events') WHERE name = 'source_path'":                                              0,
				"SELECT count(*) FROM sqlite_master WHERE type = 'trigger' AND tbl_name = 'events'":                                        3,
				"SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('events_session_ts', 'events_ts')":                   2,
				"SELECT count(*) FROM sqlite_master WHERE type = 'view'":                                                                   2,
				"SELECT count(*) FROM sqlite_master WHERE name IN ('events_v5', 'sources_v5')":                                             0,
			}
			for q, want := range checks {
				if got := countRows(t, s, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
			if _, err := s.Session(ctx, "claude:s1"); err != nil {
				t.Errorf("Session after migration: %v", err)
			}
			// The read path resolves the source path through the join.
			evs, err := s.Events(ctx, "claude:s1", true)
			if err != nil || len(evs) != 3 || evs[0].SourcePath != "/p/a.jsonl" || evs[1].SourcePath != "/p/gone.jsonl" || evs[1].SourceOffset != 10 {
				t.Errorf("Events after migration = %+v (%v)", evs, err)
			}
			// UserMessages decompresses raw for reindex --actors.
			var raws []string
			_ = s.UserMessages(ctx, "claude", func(m UserMessage) error { raws = append(raws, m.Raw); return nil })
			if len(raws) != 1 || raws[0] != sampleRawTrimmed {
				t.Errorf("UserMessages raw = %q", raws)
			}
			// New rows and rewrites keep working on the rebuilt table (triggers, FK).
			if err := s.WriteBatch(ctx, SourceState{Path: "/p/a.jsonl", Harness: "claude", Size: 200, Offset: 200}, nil,
				[]model.Event{{ID: "claude:e4", SessionID: "claude:s1", TS: ts("2026-10-01T12:00:00Z"), Kind: model.KindMessage, Role: "user",
					Actor: model.ActorHuman, Text: "after the migration", SourcePath: "/p/a.jsonl", SourceOffset: 100, Raw: `{"type":"user"}`}}); err != nil {
				t.Fatalf("WriteBatch after migration: %v", err)
			}
			if n := countRows(t, s, "SELECT count(*) FROM events_fts WHERE events_fts MATCH 'after'"); n != 1 {
				t.Errorf("FTS after migration = %d", n)
			}
			if k < LatestSchemaVersion() {
				joined := strings.Join(logged, "\n")
				if !strings.Contains(joined, "migration 5: 4/4 rows rewritten") {
					t.Errorf("progress not logged: %q", logged)
				}
			}
			// Migration 4: the link and account columns are usable and indexed,
			// and the reverse link is derived from them.
			if _, err := s.db().ExecContext(ctx, `UPDATE sessions SET continued_in = 'claude:s1', account_id = 'acc-1',
				organization_id = 'org-1' WHERE id = 'claude:s0'`); err != nil {
				t.Fatalf("set v4 columns: %v", err)
			}
			v4 := map[string]int{
				"SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('sessions_continued_in', 'sessions_account_id')": 2,
				"SELECT count(*) FROM sessions WHERE continued_in IS NULL AND account_id IS NULL AND organization_id IS NULL":          2,
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

// TestMigrationNotes: every migration has a note for `migrate`, and the
// long one says what it costs.
func TestMigrationNotes(t *testing.T) {
	if len(migrationNotes) != len(migrations) {
		t.Fatalf("%d notes for %d migrations", len(migrationNotes), len(migrations))
	}
	for i, n := range migrationNotes {
		if strings.TrimSpace(n) == "" {
			t.Errorf("migration %d has no note", i+1)
		}
	}
	for _, want := range []string{"raw", "zstd", "source_id", "minute", "disk space"} {
		if !strings.Contains(migrationNotes[4], want) {
			t.Errorf("note of migration 5 lacks %q: %s", want, migrationNotes[4])
		}
	}
}
