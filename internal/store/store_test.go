package store

import (
	"bytes"
	"compress/gzip"
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
	if err := s.db().QueryRowContext(ctx, q).Scan(&n); err != nil {
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
		if err := s.db().QueryRowContext(ctx, "PRAGMA "+c.pragma).Scan(&got); err != nil {
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
	if _, err := s.db().ExecContext(ctx, "UPDATE schema_version SET version = 999"); err != nil {
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
	evs[3].TokensIn, evs[3].TokensCacheRead, evs[3].TokensCacheWrite, evs[3].TokensOut, evs[3].Model = 10, 6, 3, 20, "m1"
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
	if e.ID != "claude:e4" || e.TokensIn != 10 || e.TokensCacheRead != 6 || e.TokensCacheWrite != 3 || e.TokensOut != 20 || e.Model != "m1" || !e.TS.Equal(evs[3].TS) || e.SourcePath != "/src/a.jsonl" {
		t.Errorf("round trip mismatch: %+v", e)
	}
	// Cache columns: known for the event with usage, NULL for the others.
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE tokens_cache_read = 6 AND tokens_cache_write = 3"); n != 1 {
		t.Errorf("events with cache tokens = %d, want 1", n)
	}
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE tokens_in IS NULL AND (tokens_cache_read IS NOT NULL OR tokens_cache_write IS NOT NULL)"); n != 0 {
		t.Errorf("events without usage but with cache tokens = %d", n)
	}
	// Events does not load raw (#131); it is stored nevertheless, compressed.
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE id = 'claude:e4' AND raw_text(raw) = '{\"x\":1}' AND typeof(raw) = 'blob'"); n != 1 {
		t.Errorf("stored raw rows for e4 = %d, want 1", n)
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
	if filepath.Base(last) != "archive-2026-11-01.db.zst" {
		t.Errorf("last backup = %s", last)
	}
	names := listBackups(t, dir)
	want := []string{
		"archive-2026-08-31.db.zst", "archive-2026-09-30.db.zst",
		"archive-2026-10-26.db.zst", "archive-2026-10-27.db.zst", "archive-2026-10-28.db.zst", "archive-2026-10-29.db.zst",
		"archive-2026-10-30.db.zst", "archive-2026-10-31.db.zst", "archive-2026-11-01.db.zst",
	}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("kept:\n got %v\nwant %v", names, want)
	}
	// A second backup on the same day is a further copy.
	if _, err := s.Backup(ctx, dir, ts("2026-11-01T22:00:00Z"), 7, 0); err != nil {
		t.Fatal(err)
	}
	// keep_monthly limits the monthly copies; the current month counts.
	if _, err := s.Backup(ctx, dir, ts("2026-11-01T22:30:00Z"), 2, 2); err != nil {
		t.Fatal(err)
	}
	want = []string{"archive-2026-10-31.db.zst", "archive-2026-11-01-2200.db.zst", "archive-2026-11-01-2230.db.zst", "archive-2026-11-01.db.zst"}
	if got := listBackups(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("kept with keep_monthly=2:\n got %v\nwant %v", got, want)
	}
	// A backup restores to a valid, complete database.
	restored := filepath.Join(t.TempDir(), "restored.db")
	if err := Restore(ctx, filepath.Join(dir, "archive-2026-11-01.db.zst"), restored, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	b, err := OpenReadOnly(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	hits, err := b.Search(ctx, "cookie", SearchFilter{})
	if err != nil || len(hits) != 1 {
		t.Errorf("search in backup: %v %v", hits, err)
	}
	p, mt, err := LastBackup(dir, time.UTC)
	if err != nil || filepath.Base(p) != "archive-2026-11-01-2230.db.zst" || mt.IsZero() {
		t.Errorf("LastBackup = %q %v %v", p, mt, err)
	}
	if p, _, err := LastBackup(filepath.Join(t.TempDir(), "none"), time.UTC); err != nil || p != "" {
		t.Errorf("LastBackup on missing dir = %q %v", p, err)
	}
}

func TestBackupLegacyFormats(t *testing.T) {
	s, _ := openTestStore(t)
	dir := t.TempDir()
	// Uncompressed copies (pre-release builds) and gzip copies (up to v1.2).
	for _, n := range []string{"archive-2026-09-30.db", "archive-2026-10-29.db", "archive-2026-10-30.db.gz", "archive-2026-10-31.db.gz", "archive-2026-11-01.db", "archive-2026-11-01.db.gz", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, _, err := LastBackup(dir, time.UTC)
	if err != nil || filepath.Base(p) != "archive-2026-11-01.db.gz" {
		t.Errorf("LastBackup with legacy files = %q %v", p, err)
	}
	// Today's legacy copies stay next to today's zstd backup; retention
	// counts all formats alike.
	if _, err := s.Backup(ctx, dir, ts("2026-11-01T22:00:00Z"), 2, 0); err != nil {
		t.Fatal(err)
	}
	want := []string{"archive-2026-09-30.db", "archive-2026-10-31.db.gz", "archive-2026-11-01-2200.db.zst", "archive-2026-11-01.db", "archive-2026-11-01.db.gz", "notes.txt"}
	if got := listBackups(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("kept:\n got %v\nwant %v", got, want)
	}
	for _, n := range []string{".tmp"} {
		if m, _ := filepath.Glob(filepath.Join(dir, "*"+n)); len(m) > 0 {
			t.Errorf("temp files left: %v", m)
		}
	}
	if p, _, err := LastBackup(dir, time.UTC); err != nil || filepath.Base(p) != "archive-2026-11-01-2200.db.zst" {
		t.Errorf("LastBackup = %q %v", p, err)
	}
}

func TestRestore(t *testing.T) {
	s, dbPath := openTestStore(t)
	sess, evs := sampleBatch()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	zst, err := s.Backup(ctx, dir, ts("2026-11-01T22:00:00Z"), 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Legacy copies restore too: uncompressed (pre-release builds) and gzip
	// (up to v1.2).
	legacy := filepath.Join(dir, "archive-2026-10-01.db")
	if _, err := s.db().ExecContext(ctx, "VACUUM INTO ?", legacy); err != nil {
		t.Fatal(err)
	}
	gz := filepath.Join(dir, "archive-2026-10-02.db.gz")
	gzipFileForTest(t, legacy, gz)
	notDB := filepath.Join(dir, "garbage.db.zst")
	if err := os.WriteFile(notDB, []byte("this is not a zstd file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	notGz := filepath.Join(dir, "garbage.db.gz")
	if err := os.WriteFile(notGz, []byte("this is not a gzip file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(existing, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		src     string
		dst     string
		force   bool
		wantErr string
	}{
		{"zstd to new path", zst, filepath.Join(t.TempDir(), "a", "new.db"), false, ""},
		{"gzip to new path", gz, filepath.Join(t.TempDir(), "new.db"), false, ""},
		{"legacy to new path", legacy, filepath.Join(t.TempDir(), "new.db"), false, ""},
		{"existing target refused", zst, existing, false, "exists"},
		{"live database refused", zst, dbPath, false, "exists"},
		{"not a zstd backup", notDB, filepath.Join(t.TempDir(), "x.db"), false, "magic number"},
		{"not a gzip backup", notGz, filepath.Join(t.TempDir(), "x.db"), false, "gzip"},
		{"missing source", filepath.Join(dir, "nope.db.zst"), filepath.Join(t.TempDir(), "y.db"), false, "no such file"},
		{"force replaces", zst, existing, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := os.ReadFile(tt.dst)
			err := Restore(ctx, tt.src, tt.dst, tt.force)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Restore = %v, want error containing %q", err, tt.wantErr)
				}
				after, _ := os.ReadFile(tt.dst)
				if string(before) != string(after) {
					t.Errorf("target changed on error")
				}
				if m, _ := filepath.Glob(filepath.Join(filepath.Dir(tt.dst), "*.restore-*")); len(m) > 0 {
					t.Errorf("temp files left: %v", m)
				}
				return
			}
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			r, err := OpenReadOnly(tt.dst)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			if n := countRows(t, r, "SELECT count(*) FROM events"); n != len(evs) {
				t.Errorf("events = %d", n)
			}
		})
	}
}

// gzipFileForTest writes src gzip-compressed to dst, the backup format of
// up to v1.2.
func gzipFileForTest(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
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
			if err := s.db().QueryRowContext(ctx, "SELECT display_name FROM sessions WHERE id = 'claude:x'").Scan(&got); err != nil {
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

func TestWriteBatchNCountsInserted(t *testing.T) {
	s, _ := openTestStore(t)
	evs := []model.Event{
		ev("e1", "claude:s", "2026-10-01T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "a"),
		ev("e2", "claude:s", "2026-10-01T10:00:01Z", model.ActorAgent, model.KindMessage, "assistant", "b"),
	}
	for i, want := range []int{2, 0} {
		n, err := s.WriteBatchN(ctx, SourceState{}, nil, evs)
		if err != nil || n != want {
			t.Errorf("run %d: WriteBatchN = %d, %v; want %d", i, n, err, want)
		}
	}
}

func TestRawTruncatedRoundTrip(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	evs[2].Raw, evs[2].RawTruncated = "", true
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE raw_truncated = 1 AND raw IS NULL AND id = 'claude:e3'"); n != 1 {
		t.Errorf("truncated rows = %d, want 1", n)
	}
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE raw_truncated = 0"); n != len(evs)-1 {
		t.Errorf("untruncated rows = %d", n)
	}
}

func TestSessionCwdsAndSetGit(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.WriteBatch(ctx, SourceState{}, []model.Session{
		{ID: "claude:a", Harness: "claude", NativeID: "a", Cwd: "/w/a"},
		{ID: "claude:b", Harness: "claude", NativeID: "b", Cwd: "/w/a", GitRoot: "/w/a", GitRemoteURL: "https://x/a"},
		{ID: "claude:c", Harness: "claude", NativeID: "c", Cwd: "/w/c"},
		{ID: "claude:d", Harness: "claude", NativeID: "d"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	cwds, err := s.SessionCwds(ctx)
	if err != nil || strings.Join(cwds, ",") != "/w/a,/w/c" {
		t.Errorf("SessionCwds = %v %v", cwds, err)
	}
	n, err := s.SetSessionGit(ctx, "/w/a", "/w/a", "https://x/a")
	if err != nil || n != 1 {
		t.Errorf("SetSessionGit = %d %v, want 1 (one session already up to date)", n, err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM sessions WHERE git_remote_url = 'https://x/a'"); n != 2 {
		t.Errorf("updated = %d", n)
	}
}

// TestWriteBatchRewriteRaw: with RewriteRaw, existing events get the incoming
// raw and raw_truncated; text, actor, the full-text index and the sessions
// stay as they are, new events are inserted as usual (#130).
func TestWriteBatchRewriteRaw(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	src := SourceState{Path: "/src/a.jsonl", Harness: "claude", Size: 100, Offset: 100, MTime: ts("2026-10-31T22:05:00Z")}
	if err := s.WriteBatch(ctx, src, sess, evs); err != nil {
		t.Fatal(err)
	}
	counts := func() [3]int {
		return [3]int{countRows(t, s, "SELECT count(*) FROM events"), countRows(t, s, "SELECT count(*) FROM events_fts"),
			countRows(t, s, "SELECT count(*) FROM sessions")}
	}
	before := counts()
	again := make([]model.Event, len(evs))
	for i, e := range evs {
		e.Raw = `{"x":2,"text":"readable again"}`
		e.Text = "changed text must not be written"
		e.Actor = model.ActorPeer
		again[i] = e
	}
	again[1].Raw, again[1].RawTruncated = "", true
	extra := ev("claude:e9", "claude:s1", "2026-11-01T09:00:00Z", model.ActorHuman, model.KindMessage, "user", "new one")
	res, err := s.WriteBatchOpts(ctx, src, sess, append(again, extra), WriteOptions{RewriteRaw: true})
	if err != nil {
		t.Fatalf("WriteBatchOpts: %v", err)
	}
	if res.Inserted != 1 || res.RawRewritten != len(evs) {
		t.Errorf("result = %+v, want 1 inserted, %d rewritten", res, len(evs))
	}
	after := counts()
	if want := [3]int{before[0] + 1, before[1] + 1, before[2]}; after != want {
		t.Errorf("counts = %v, want %v", after, want)
	}
	got, err := s.Events(ctx, "claude:s1", true)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range got[:len(evs)] {
		if e.Text != evs[i].Text || e.Actor != evs[i].Actor {
			t.Errorf("event %s: text/actor changed: %q %q", e.ID, e.Text, e.Actor)
		}
	}
	// Events does not load raw (#131): check the columns directly.
	if n := countRows(t, s, `SELECT count(*) FROM events WHERE raw_text(raw) = '{"x":2,"text":"readable again"}' AND raw_truncated = 0`); n != len(evs)-1 {
		t.Errorf("rewritten raw rows = %d, want %d", n, len(evs)-1)
	}
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE id = 'claude:e2' AND raw_truncated = 1 AND raw IS NULL"); n != 1 {
		t.Errorf("raw_truncated rows for e2 = %d, want 1", n)
	}
	if n := countRows(t, s, "SELECT count(*) FROM events_fts WHERE text MATCH 'changed'"); n != 0 {
		t.Errorf("full-text index picked up the unwanted text: %d rows", n)
	}
	// A second run rewrites the same values again: harmless and idempotent.
	res, err = s.WriteBatchOpts(ctx, src, sess, append(again, extra), WriteOptions{RewriteRaw: true})
	if err != nil || res.Inserted != 0 || res.RawRewritten != len(evs)+1 {
		t.Errorf("second run: %+v, %v", res, err)
	}
	if got := counts(); got != after {
		t.Errorf("counts after second run = %v, want %v", got, after)
	}
}
