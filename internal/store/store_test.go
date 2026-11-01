package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

var ctx = context.Background()

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// newStore opens a fresh store in a temp dir whose name contains a space
// (like "Application Support").
func openTestStore(t *testing.T, opts ...Option) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "App Support")
	p := filepath.Join(dir, "archive.db")
	s, err := Open(p, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, p
}

func ev(id, sess, at string, actor model.Actor, kind model.Kind, role, text string) model.Event {
	return model.Event{ID: id, SessionID: sess, TS: ts(at), Actor: actor, Kind: kind, Role: role, Text: text,
		SourcePath: "/src/a.jsonl", Raw: `{"x":1}`}
}

func countRows(t *testing.T, s *Store, q string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestOpenPragmasAndSchema(t *testing.T) {
	s, p := openTestStore(t)
	checks := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "10000"},
	}
	for _, c := range checks {
		var got string
		if err := s.db.QueryRowContext(ctx, "PRAGMA "+c.pragma).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
		}
	}
	if v := countRows(t, s, "SELECT version FROM schema_version"); v != len(migrations) {
		t.Errorf("schema version = %d, want %d", v, len(migrations))
	}
	for _, name := range []string{"sources", "sessions", "events", "dirs", "events_fts", "exchanges", "daily_activity"} {
		if n := countRows(t, s, "SELECT count(*) FROM sqlite_master WHERE name='"+name+"'"); n != 1 {
			t.Errorf("missing schema object %s", name)
		}
	}
	_ = s.Close()
	// Reopen is a no-op migration.
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}

func TestOpenNewerSchemaFails(t *testing.T) {
	s, p := openTestStore(t)
	if _, err := s.db.ExecContext(ctx, "UPDATE schema_version SET version = 999"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := Open(p); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("expected newer-schema error, got %v", err)
	}
}

func TestMergeSession(t *testing.T) {
	t1, t2, t3 := ts("2026-10-01T10:00:00Z"), ts("2026-10-01T11:00:00Z"), ts("2026-10-01T12:00:00Z")
	tests := []struct {
		name     string
		old, new model.Session
		want     model.Session
	}{
		{"empty old takes new",
			model.Session{},
			model.Session{ID: "c:1", Harness: "c", Cwd: "/a", Model: "m1", StartedAt: t1, EndedAt: t1},
			model.Session{ID: "c:1", Harness: "c", Cwd: "/a", Model: "m1", StartedAt: t1, EndedAt: t1}},
		{"min start max end",
			model.Session{ID: "c:1", StartedAt: t2, EndedAt: t2},
			model.Session{ID: "c:1", StartedAt: t1, EndedAt: t3},
			model.Session{ID: "c:1", StartedAt: t1, EndedAt: t3}},
		{"zero times never win",
			model.Session{ID: "c:1", StartedAt: t1, EndedAt: t2},
			model.Session{ID: "c:1"},
			model.Session{ID: "c:1", StartedAt: t1, EndedAt: t2}},
		{"empty never overwrites",
			model.Session{ID: "c:1", Name: "n", AgentName: "a", Cwd: "/a", GitRoot: "/r", GitRemoteURL: "u", GitBranch: "b", Model: "m", HarnessVersion: "1", ParentID: "p", NativeID: "1", Harness: "c", StartedAt: t1, EndedAt: t1},
			model.Session{ID: "c:1", StartedAt: t2, EndedAt: t2},
			model.Session{ID: "c:1", Name: "n", AgentName: "a", Cwd: "/a", GitRoot: "/r", GitRemoteURL: "u", GitBranch: "b", Model: "m", HarnessVersion: "1", ParentID: "p", NativeID: "1", Harness: "c", StartedAt: t1, EndedAt: t2}},
		{"last seen from newer event",
			model.Session{ID: "c:1", GitBranch: "old", Model: "m1", HarnessVersion: "1.0", StartedAt: t1, EndedAt: t2},
			model.Session{ID: "c:1", GitBranch: "new", Model: "m2", HarnessVersion: "2.0", StartedAt: t3, EndedAt: t3},
			model.Session{ID: "c:1", GitBranch: "new", Model: "m2", HarnessVersion: "2.0", StartedAt: t1, EndedAt: t3}},
		{"last seen ignores older event",
			model.Session{ID: "c:1", GitBranch: "new", Model: "m2", StartedAt: t1, EndedAt: t3},
			model.Session{ID: "c:1", GitBranch: "old", Model: "m1", StartedAt: t2, EndedAt: t2},
			model.Session{ID: "c:1", GitBranch: "new", Model: "m2", StartedAt: t1, EndedAt: t3}},
		{"last seen without time fills only empty",
			model.Session{ID: "c:1", Model: "m1", StartedAt: t1, EndedAt: t1},
			model.Session{ID: "c:1", Model: "m2", GitBranch: "b"},
			model.Session{ID: "c:1", Model: "m1", GitBranch: "b", StartedAt: t1, EndedAt: t1}},
		{"first seen cwd keeps earliest",
			model.Session{ID: "c:1", Cwd: "/later", GitRoot: "/later", StartedAt: t2, EndedAt: t2},
			model.Session{ID: "c:1", Cwd: "/first", GitRoot: "/first", StartedAt: t1, EndedAt: t1},
			model.Session{ID: "c:1", Cwd: "/first", GitRoot: "/first", StartedAt: t1, EndedAt: t2}},
		{"first seen cwd ignores later",
			model.Session{ID: "c:1", Cwd: "/first", StartedAt: t1, EndedAt: t1},
			model.Session{ID: "c:1", Cwd: "/later", GitRemoteURL: "u", StartedAt: t2, EndedAt: t2},
			model.Session{ID: "c:1", Cwd: "/first", GitRemoteURL: "u", StartedAt: t1, EndedAt: t2}},
		{"name last write wins",
			model.Session{ID: "c:1", Name: "old title"},
			model.Session{ID: "c:1", Name: "new title"},
			model.Session{ID: "c:1", Name: "new title"}},
		{"first seen identity",
			model.Session{ID: "c:1", ParentID: "c:p", NativeID: "1", Harness: "c"},
			model.Session{ID: "c:1", ParentID: "c:other", NativeID: "x", Harness: "y"},
			model.Session{ID: "c:1", ParentID: "c:p", NativeID: "1", Harness: "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeSession(tt.old, tt.new); got != tt.want {
				t.Errorf("mergeSession:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func sampleBatch() ([]model.Session, []model.Event) {
	sess := []model.Session{{ID: "claude:s1", Harness: "claude", NativeID: "s1", Name: "Fix login", Cwd: "/w/app",
		GitRoot: "/w/app", GitRemoteURL: "https://github.com/acme/app", GitBranch: "main", Model: "m1",
		StartedAt: ts("2026-10-31T22:00:00Z"), EndedAt: ts("2026-10-31T22:05:00Z")}}
	evs := []model.Event{
		ev("claude:e1", "claude:s1", "2026-10-31T22:00:00Z", model.ActorHuman, model.KindMessage, "user", "please fix the login bug"),
		ev("claude:e2", "claude:s1", "2026-10-31T22:01:00Z", model.ActorAgent, model.KindToolUse, "assistant", "grep login"),
		ev("claude:e3", "claude:s1", "2026-10-31T22:02:00Z", model.ActorTool, model.KindToolResult, "tool", "login.go:12"),
		ev("claude:e4", "claude:s1", "2026-10-31T22:03:00Z", model.ActorAgent, model.KindMessage, "assistant", "looking at it"),
		ev("claude:e5", "claude:s1", "2026-10-31T22:04:00Z", model.ActorAgent, model.KindMessage, "assistant", "fixed the session cookie"),
		ev("claude:e6", "claude:s1", "2026-10-31T22:05:00Z", model.ActorHuman, model.KindMessage, "user", "thanks, now the tests"),
	}
	evs[1].ToolName = "Bash"
	evs[3].TokensIn, evs[3].TokensOut, evs[3].Model = 10, 20, "m1"
	return sess, evs
}

func TestWriteBatchIdempotent(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	src := SourceState{Path: "/src/a.jsonl", Harness: "claude", Size: 100, Offset: 100, MTime: ts("2026-10-31T22:05:00Z")}
	for i := 0; i < 2; i++ {
		if err := s.WriteBatch(ctx, src, sess, evs); err != nil {
			t.Fatalf("WriteBatch #%d: %v", i, err)
		}
	}
	if n := countRows(t, s, "SELECT count(*) FROM events"); n != len(evs) {
		t.Errorf("events = %d, want %d", n, len(evs))
	}
	if n := countRows(t, s, "SELECT count(*) FROM events_fts"); n != len(evs) {
		t.Errorf("fts rows = %d, want %d", n, len(evs))
	}
	if n := countRows(t, s, "SELECT count(*) FROM sessions"); n != 1 {
		t.Errorf("sessions = %d", n)
	}
	got, err := s.Events(ctx, "claude:s1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(evs) {
		t.Fatalf("Events = %d", len(got))
	}
	e := got[3]
	if e.ID != "claude:e4" || e.TokensIn != 10 || e.TokensOut != 20 || e.Model != "m1" || e.Raw != `{"x":1}` || !e.TS.Equal(evs[3].TS) || e.SourcePath != "/src/a.jsonl" {
		t.Errorf("round trip mismatch: %+v", e)
	}
	if got[1].ToolName != "Bash" {
		t.Errorf("tool name = %q", got[1].ToolName)
	}
}

func TestWriteBatchMergesAcrossBatches(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs[:2]); err != nil {
		t.Fatal(err)
	}
	later := []model.Session{{ID: "claude:s1", GitBranch: "feature", Model: "m2", StartedAt: ts("2026-11-01T09:00:00Z"), EndedAt: ts("2026-11-01T09:00:00Z")}}
	extra := ev("claude:e7", "claude:s1", "2026-11-01T09:00:00Z", model.ActorHuman, model.KindMessage, "user", "next day")
	if err := s.WriteBatch(ctx, SourceState{}, later, []model.Event{extra}); err != nil {
		t.Fatal(err)
	}
	// An event for an unknown session creates a minimal session row.
	orphan := ev("claude:o1", "claude:sub", "2026-11-01T08:00:00Z", model.ActorAgent, model.KindMessage, "assistant", "sub")
	if err := s.WriteBatch(ctx, SourceState{}, nil, []model.Event{orphan}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Session(ctx, "claude:s1")
	if err != nil {
		t.Fatal(err)
	}
	want := sess[0]
	want.GitBranch, want.Model, want.EndedAt = "feature", "m2", ts("2026-11-01T09:00:00Z")
	if got.Session != want {
		t.Errorf("merged session:\n got %+v\nwant %+v", got.Session, want)
	}
	sub, err := s.Session(ctx, "claude:sub")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Harness != "claude" || sub.NativeID != "sub" || !sub.StartedAt.Equal(orphan.TS) || !sub.EndedAt.Equal(orphan.TS) {
		t.Errorf("orphan session = %+v", sub.Session)
	}
}

func TestSourceOffsets(t *testing.T) {
	s, _ := openTestStore(t, WithClock(func() time.Time { return ts("2026-11-01T12:00:00Z") }))
	if _, ok, err := s.Source(ctx, "/src/a.jsonl"); err != nil || ok {
		t.Fatalf("Source on empty db: ok=%v err=%v", ok, err)
	}
	steps := []SourceState{
		{Path: "/src/a.jsonl", Harness: "claude", Size: 50, Offset: 40, MTime: ts("2026-11-01T10:00:00Z")},
		{Path: "/src/a.jsonl", Harness: "claude", Size: 120, Offset: 120, MTime: ts("2026-11-01T11:00:00Z")},
		{Path: "/src/a.jsonl", Harness: "claude", Size: 10, Offset: 0, MTime: ts("2026-11-01T11:30:00Z")}, // truncated, re-read from 0
	}
	for i, st := range steps {
		if err := s.WriteBatch(ctx, st, nil, nil); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.Source(ctx, st.Path)
		if err != nil || !ok {
			t.Fatalf("step %d: ok=%v err=%v", i, ok, err)
		}
		if got.Offset != st.Offset || got.Size != st.Size || !got.MTime.Equal(st.MTime) || got.Harness != "claude" ||
			!got.IngestedAt.Equal(ts("2026-11-01T12:00:00Z")) {
			t.Errorf("step %d: got %+v", i, got)
		}
	}
}

func TestWriteBatchIsAtomic(t *testing.T) {
	s, _ := openTestStore(t)
	bad := ev("claude:e1", "", "2026-11-01T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "x")
	src := SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 99}
	if err := s.WriteBatch(ctx, src, nil, []model.Event{bad}); err == nil {
		t.Fatal("expected error for event without session")
	}
	if _, ok, _ := s.Source(ctx, "/src/a.jsonl"); ok {
		t.Error("offset was written although the batch failed")
	}
}

func TestDirs(t *testing.T) {
	s, _ := openTestStore(t)
	if _, ok, err := s.Dir(ctx, "/w/x"); err != nil || ok {
		t.Fatalf("Dir empty: %v %v", ok, err)
	}
	d := DirInfo{Cwd: "/w/x", GitRoot: "/w", GitRemoteURL: "https://github.com/a/b", ResolvedAt: ts("2026-11-01T10:00:00Z")}
	if err := s.PutDir(ctx, d); err != nil {
		t.Fatal(err)
	}
	d.GitRemoteURL = "https://github.com/a/c"
	if err := s.PutDir(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Dir(ctx, "/w/x")
	if err != nil || !ok || got != d {
		t.Errorf("Dir = %+v %v %v, want %+v", got, ok, err, d)
	}
}

func TestReadOnly(t *testing.T) {
	s, p := openTestStore(t)
	sess, evs := sampleBatch()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = ro.Close() }()
	if _, _, err := ro.Query(ctx, "SELECT count(*) FROM events"); err != nil {
		t.Errorf("read failed: %v", err)
	}
	for _, q := range []string{"DELETE FROM events", "INSERT INTO dirs(cwd) VALUES('x')", "DROP TABLE events", "PRAGMA query_only=0; DELETE FROM events"} {
		if _, _, err := ro.Query(ctx, q); err == nil {
			t.Errorf("write %q succeeded on read-only store", q)
		}
	}
	if err := ro.WriteBatch(ctx, SourceState{Path: "/x"}, nil, nil); err == nil {
		t.Error("WriteBatch succeeded on read-only store")
	}
	if _, err := OpenReadOnly(filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Error("OpenReadOnly of missing file succeeded")
	}
	if n := countRows(t, ro, "SELECT count(*) FROM events"); n != len(evs) {
		t.Errorf("events after attempted writes = %d", n)
	}
}

func TestQuery(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	cols, rows, err := s.Query(ctx, "SELECT actor, count(*) AS n FROM events GROUP BY actor ORDER BY actor")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cols, ",") != "actor,n" {
		t.Errorf("cols = %v", cols)
	}
	if len(rows) != 3 || rows[0][0] != "agent" || rows[0][1] != int64(3) {
		t.Errorf("rows = %v", rows)
	}
	if _, _, err := s.Query(ctx, "SELEC nope"); err == nil {
		t.Error("expected syntax error")
	}
}

func TestBackupRetention(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "backups")
	// Daily backups from 2026-08-25 to 2026-11-01.
	start := ts("2026-08-25T20:00:00Z")
	var last string
	for d := 0; ; d++ {
		now := start.AddDate(0, 0, d)
		if now.After(ts("2026-11-01T21:00:00Z")) {
			break
		}
		p, err := s.Backup(ctx, dir, now, 7, 0)
		if err != nil {
			t.Fatalf("Backup %s: %v", now, err)
		}
		last = p
	}
	if filepath.Base(last) != "archive-2026-11-01.db" {
		t.Errorf("last backup = %s", last)
	}
	names := listBackups(t, dir)
	want := []string{
		"archive-2026-08-31.db", "archive-2026-09-30.db",
		"archive-2026-10-26.db", "archive-2026-10-27.db", "archive-2026-10-28.db", "archive-2026-10-29.db",
		"archive-2026-10-30.db", "archive-2026-10-31.db", "archive-2026-11-01.db",
	}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("kept:\n got %v\nwant %v", names, want)
	}
	// Second backup on the same day replaces the file.
	if _, err := s.Backup(ctx, dir, ts("2026-11-01T22:00:00Z"), 7, 0); err != nil {
		t.Fatal(err)
	}
	// keep_monthly limits the monthly copies; the current month counts.
	if _, err := s.Backup(ctx, dir, ts("2026-11-01T22:30:00Z"), 2, 2); err != nil {
		t.Fatal(err)
	}
	want = []string{"archive-2026-10-31.db", "archive-2026-11-01.db"}
	if got := listBackups(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("kept with keep_monthly=2:\n got %v\nwant %v", got, want)
	}
	// A backup is a valid, complete database.
	b, err := OpenReadOnly(filepath.Join(dir, "archive-2026-11-01.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	hits, err := b.Search(ctx, "cookie", SearchFilter{})
	if err != nil || len(hits) != 1 {
		t.Errorf("search in backup: %v %v", hits, err)
	}
	p, mt, err := LastBackup(dir)
	if err != nil || filepath.Base(p) != "archive-2026-11-01.db" || mt.IsZero() {
		t.Errorf("LastBackup = %q %v %v", p, mt, err)
	}
	if p, _, err := LastBackup(filepath.Join(t.TempDir(), "none")); err != nil || p != "" {
		t.Errorf("LastBackup on missing dir = %q %v", p, err)
	}
}

func listBackups(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestOpenReadOnlyMissingIsNotCreated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	_, err := OpenReadOnly(p)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, statErr := os.Stat(p); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("read-only open created the database file")
	}
}

func TestConcurrentWriters(t *testing.T) {
	_, p := openTestStore(t)
	const writers, batches = 4, 25
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			s, err := Open(p)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = s.Close() }()
			for b := 0; b < batches; b++ {
				id := fmt.Sprintf("claude:w%d-b%d", w, b)
				e := ev(id, "claude:shared", "2026-11-01T10:00:00Z", model.ActorAgent, model.KindMessage, "assistant", "x")
				src := SourceState{Path: fmt.Sprintf("/src/%d.jsonl", w), Harness: "claude", Offset: int64(b)}
				if err := s.WriteBatch(ctx, src, nil, []model.Event{e}); err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}(w)
	}
	for w := 0; w < writers; w++ {
		if err := <-errs; err != nil {
			t.Fatalf("writer: %v", err)
		}
	}
	s, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if n := countRows(t, s, "SELECT count(*) FROM events"); n != writers*batches {
		t.Errorf("events = %d, want %d", n, writers*batches)
	}
}

func TestDisplayNameAndAITitle(t *testing.T) {
	tests := []struct {
		name    string
		batches []model.Session
		want    string
	}{
		{"agent name only", []model.Session{{AgentName: "agent"}}, "agent"},
		{"ai title beats agent name", []model.Session{{AgentName: "agent"}, {AITitle: "generated"}}, "generated"},
		{"custom title beats ai title", []model.Session{{Name: "custom"}, {AITitle: "generated"}}, "custom"},
		{"later ai title replaces earlier", []model.Session{{AITitle: "one"}, {AITitle: "two"}}, "two"},
		{"nothing", []model.Session{{Cwd: "/w"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := openTestStore(t)
			for _, b := range tt.batches {
				b.ID, b.Harness, b.NativeID = "claude:x", "claude", "x"
				if err := s.WriteBatch(ctx, SourceState{}, []model.Session{b}, nil); err != nil {
					t.Fatal(err)
				}
			}
			var got sql.NullString
			if err := s.db.QueryRowContext(ctx, "SELECT display_name FROM sessions WHERE id = 'claude:x'").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got.String != tt.want {
				t.Errorf("display_name = %q, want %q", got.String, tt.want)
			}
			si, err := s.Session(ctx, "claude:x")
			if err != nil {
				t.Fatal(err)
			}
			if si.DisplayName() != tt.want {
				t.Errorf("DisplayName() = %q, want %q", si.DisplayName(), tt.want)
			}
		})
	}
}
