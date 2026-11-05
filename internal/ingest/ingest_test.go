package ingest

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/redact"
	"github.com/seletz/total-recall/internal/store"
	tf "github.com/seletz/total-recall/internal/testfixture"
)

var ctx = context.Background()

const sid = tf.SmallSession1

// fakeGit counts Resolve calls per cwd.
type fakeGit struct {
	mu    sync.Mutex
	calls map[string]int
	infos map[string]gitinfo.Info
}

func (g *fakeGit) resolve(_ context.Context, cwd string) (gitinfo.Info, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.calls == nil {
		g.calls = map[string]int{}
	}
	g.calls[cwd]++
	if i, ok := g.infos[cwd]; ok {
		return i, nil
	}
	return gitinfo.Info{}, gitinfo.ErrNotRepo
}

type env struct {
	dir      string
	projects string
	dbPath   string
	st       *store.Store
	git      *fakeGit
	deps     Deps
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{dir: dir, projects: filepath.Join(dir, "projects"), dbPath: filepath.Join(dir, "db", "archive.db"),
		git: &fakeGit{infos: map[string]gitinfo.Info{
			"/w/alpha": {Root: "/w/alpha", Toplevel: "/w/alpha", RemoteURL: "https://github.com/o/alpha"},
			"/w/wt":    {Root: "/w/main", Toplevel: "/w/wt", RemoteURL: "https://github.com/o/main"},
		}}}
	e.open(t)
	return e
}

func (e *env) open(t *testing.T) {
	t.Helper()
	st, err := store.Open(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e.st = st
	e.deps = Deps{
		Store:      st,
		Adapters:   []harness.Adapter{claude.New(e.projects)},
		StoreRaw:   true,
		ResolveGit: true,
		GitResolve: e.git.resolve,
		LockDir:    filepath.Join(filepath.Dir(e.dbPath), "locks"),
	}
}

func (e *env) count(t *testing.T, q string) int {
	t.Helper()
	_, rows, err := e.st.Query(ctx, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return int(rows[0][0].(int64))
}

func (e *env) str(t *testing.T, q string) string {
	t.Helper()
	_, rows, err := e.st.Query(ctx, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	if len(rows) == 0 || rows[0][0] == nil {
		return ""
	}
	return rows[0][0].(string)
}

func run(t *testing.T, e *env, paths ...string) Summary {
	t.Helper()
	sum, err := Run(ctx, e.deps, paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return sum
}

func TestAllSmallTree(t *testing.T) {
	e := newEnv(t)
	tf.Small(t, e.dir)
	sum, err := All(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Scanned != 4 || sum.Changed != 4 || len(sum.Errors) != 0 {
		t.Errorf("summary = %+v", sum)
	}
	if got := e.count(t, "SELECT count(*) FROM events"); got != sum.Events || got != 9 {
		t.Errorf("events = %d, summary %d, want 9", got, sum.Events)
	}
	// Three sessions with events plus the workflow agent named by its journal.
	if sum.Sessions != 4 || sum.Meta != 1 {
		t.Errorf("sessions touched = %d, meta records = %d, want 4 and 1", sum.Sessions, sum.Meta)
	}
	// The workflow journal yields a meta record on the agent's session (no
	// events, no times) and its offset is recorded.
	if got := e.count(t, `SELECT count(*) FROM sources WHERE path LIKE '%journal.jsonl' AND "offset" > 0`); got != 1 {
		t.Errorf("journal source rows = %d", got)
	}
	if got := e.count(t, `SELECT count(*) FROM session_meta m JOIN sessions s ON s.id = m.session_id
		WHERE m.type = 'started' AND s.id = 'claude:agent-a02' AND s.parent_id = 'claude:`+sid+`' AND s.started_at IS NULL`); got != 1 {
		t.Errorf("journal meta rows = %d", got)
	}
	if got := e.str(t, "SELECT display_name FROM sessions WHERE native_id = '"+sid+"'"); got != "List files" {
		t.Errorf("display name = %q", got)
	}
	if got := e.str(t, "SELECT git_remote_url FROM sessions WHERE native_id = '"+sid+"'"); got != "https://github.com/o/alpha" {
		t.Errorf("git remote = %q", got)
	}

	again, err := All(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	if again.Events != 0 || again.Changed != 0 || again.Unchanged != 4 {
		t.Errorf("second run = %+v", again)
	}
}

func TestIncrementalPartialLine(t *testing.T) {
	e := newEnv(t)
	full := tf.Join(
		tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/x", "first"),
		tf.Assistant(sid, "u2", "2026-10-01T10:00:01Z", "/w/x", "second"),
		tf.User(sid, "u3", "2026-10-01T10:00:02Z", "/w/x", "third"),
	)
	cut := len(full) - 20 // inside the last line
	p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl", full[:cut])

	if sum := run(t, e, p); sum.Events != 2 {
		t.Fatalf("first run events = %d, want 2", sum.Events)
	}
	tf.AppendFile(t, p, full[cut:])
	if sum := run(t, e, p); sum.Events != 1 {
		t.Fatalf("second run events = %d, want 1", sum.Events)
	}
	if got := e.count(t, "SELECT count(*) FROM events"); got != 3 {
		t.Errorf("events = %d", got)
	}
	if got := e.count(t, `SELECT "offset" FROM sources`); got != len(full) {
		t.Errorf("offset = %d, want %d", got, len(full))
	}
}

func TestTruncatedFileRestartsAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	content := tf.Join(
		tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/x", "first"),
		tf.Assistant(sid, "u2", "2026-10-01T10:00:01Z", "/w/x", "second"),
	)
	p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl", content)
	run(t, e, p)

	// Replaced by a shorter file with a new line: re-read from 0.
	short := tf.User(sid, "n1", "2026-10-02T10:00:00Z", "/w/x", "new").JSON()
	if err := os.WriteFile(p, []byte(short), 0o644); err != nil {
		t.Fatal(err)
	}
	if sum := run(t, e, p); sum.Events != 1 {
		t.Errorf("after truncation events = %d, want 1", sum.Events)
	}
	// Restore the original content: offset (short) < size, but re-reading the
	// original lines must not duplicate anything.
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, e, p)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if sum := run(t, e, p); sum.Events != 0 {
		t.Errorf("re-read of known lines inserted %d events", sum.Events)
	}
	if got := e.count(t, "SELECT count(*) FROM events"); got != 3 {
		t.Errorf("events = %d, want 3", got)
	}
}

func TestBatchingWritesEverything(t *testing.T) {
	e := newEnv(t)
	e.deps.BatchSize = 2
	var lines []tf.Line
	for i := range 7 {
		lines = append(lines, tf.User(sid, "u"+string(rune('a'+i)), "2026-10-01T10:00:0"+string(rune('0'+i))+"Z", "/w/x", "msg"))
	}
	p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl", tf.Join(lines...))
	if sum := run(t, e, p); sum.Events != 7 {
		t.Errorf("events = %d, want 7", sum.Events)
	}
	if got := e.count(t, `SELECT "offset" FROM sources`); got != len(tf.Join(lines...)) {
		t.Errorf("offset = %d", got)
	}
}

func TestLockedFileIsSkipped(t *testing.T) {
	e := newEnv(t)
	p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl", tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/x", "hi").JSON())
	unlock, ok, err := lockFile(e.deps.LockDir, p)
	if err != nil || !ok {
		t.Fatalf("lockFile = %v, %v", ok, err)
	}
	sum := run(t, e, p)
	if sum.Locked != 1 || sum.Events != 0 {
		t.Errorf("summary while locked = %+v", sum)
	}
	unlock()
	if sum := run(t, e, p); sum.Locked != 0 || sum.Events != 1 {
		t.Errorf("summary after unlock = %+v", sum)
	}
}

func TestRedactionAndStoreRaw(t *testing.T) {
	secret := "ghp_" + strings.Repeat("aB3", 12)
	tests := []struct {
		name     string
		redact   bool
		storeRaw bool
	}{
		{"redacted with raw", true, true},
		{"redacted without raw", true, false},
		{"not redacted", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.redact {
				r, err := redact.New(redact.Options{})
				if err != nil {
					t.Fatal(err)
				}
				e.deps.Redactor = r
			}
			e.deps.StoreRaw = tt.storeRaw
			p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl",
				tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/x", "my token is "+secret).JSON())
			run(t, e, p)
			text := e.str(t, "SELECT text FROM events")
			raw := e.str(t, "SELECT raw_text(raw) FROM events")
			if tt.redact == strings.Contains(text, secret) {
				t.Errorf("text = %q", text)
			}
			if !tt.storeRaw {
				if raw != "" {
					t.Errorf("raw stored although store_raw=false: %q", raw)
				}
				return
			}
			if raw == "" || tt.redact == strings.Contains(raw, secret) {
				t.Errorf("raw = %q", raw)
			}
		})
	}
}

func TestGitInfoCache(t *testing.T) {
	e := newEnv(t)
	p := tf.WriteFile(t, e.projects, "-w-wt/"+sid+".jsonl", tf.Join(
		tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/wt", "a"),
		tf.Assistant(sid, "u2", "2026-10-01T10:00:01Z", "/w/wt", "b"),
		tf.User(sid, "u3", "2026-10-01T10:00:02Z", "/w/none", "c"),
	))
	run(t, e, p)
	if e.git.calls["/w/wt"] != 1 || e.git.calls["/w/none"] != 1 {
		t.Errorf("resolve calls = %v", e.git.calls)
	}
	// Worktree: git_root is the main repository root.
	if got := e.str(t, "SELECT git_root FROM sessions"); got != "/w/main" {
		t.Errorf("git_root = %q", got)
	}
	if got := e.count(t, "SELECT count(*) FROM dirs"); got != 2 {
		t.Errorf("dirs rows = %d", got)
	}

	// A new run (new process) uses the dirs table instead of git.
	tf.AppendFile(t, p, tf.User(sid, "u4", "2026-10-01T10:00:03Z", "/w/wt", "d").JSON())
	e.git.calls = nil
	run(t, e, p)
	if len(e.git.calls) != 0 {
		t.Errorf("resolve calls with cache = %v", e.git.calls)
	}

	// git.resolve=false: no resolution at all.
	e2 := newEnv(t)
	e2.deps.ResolveGit = false
	p2 := tf.WriteFile(t, e2.projects, "-w-wt/"+sid+".jsonl", tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/wt", "a").JSON())
	run(t, e2, p2)
	if len(e2.git.calls) != 0 || e2.str(t, "SELECT git_root FROM sessions") != "" {
		t.Errorf("git resolved although disabled: %v", e2.git.calls)
	}
}

func TestPerFileErrorsContinue(t *testing.T) {
	e := newEnv(t)
	good := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl", tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/x", "hi").JSON())
	missing := filepath.Join(e.projects, "nope.jsonl")
	sum := run(t, e, missing, good)
	if len(sum.Errors) != 1 || sum.Errors[0].Path != missing || sum.Events != 1 {
		t.Errorf("summary = %+v", sum)
	}
	if !errors.Is(sum.Errors[0].Err, os.ErrNotExist) {
		t.Errorf("error = %v", sum.Errors[0].Err)
	}
}

func TestToolResultMaxBytesIsAdapterOption(t *testing.T) {
	// The orchestration passes events through unchanged in size; truncation is
	// the adapter's job (configured via claude.WithToolResultMaxBytes).
	e := newEnv(t)
	e.deps.Adapters = []harness.Adapter{claude.New(e.projects, claude.WithToolResultMaxBytes(10))}
	line := tf.Line{"type": "user", "uuid": "r1", "timestamp": "2026-10-01T10:00:00Z", "sessionId": sid, "cwd": "/w/x",
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": strings.Repeat("x", 100)}}}}
	p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl", line.JSON())
	run(t, e, p)
	if got := e.str(t, "SELECT text FROM events"); !strings.HasPrefix(got, "xxxxxxxxxx") || len(got) > 40 {
		t.Errorf("text = %q", got)
	}
}

func TestGitNegativeCacheExpires(t *testing.T) {
	e := newEnv(t)
	old := time.Now().Add(-2 * NegativeDirTTL)
	for _, d := range []store.DirInfo{
		{Cwd: "/w/alpha", ResolvedAt: old},                    // stale "not a repo": resolve again
		{Cwd: "/w/fresh", ResolvedAt: time.Now()},             // fresh "not a repo": keep
		{Cwd: "/w/wt", GitRoot: "/w/cached", ResolvedAt: old}, // positive: keep forever
	} {
		if err := e.st.PutDir(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	p := tf.WriteFile(t, e.projects, "-w/"+sid+".jsonl", tf.Join(
		tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/alpha", "a"),
		tf.User(tf.SmallSession2, "u2", "2026-10-01T10:00:00Z", "/w/fresh", "b"),
		tf.User("33333333-3333-4333-8333-333333333333", "u3", "2026-10-01T10:00:00Z", "/w/wt", "c"),
	))
	run(t, e, p)
	if e.git.calls["/w/alpha"] != 1 || e.git.calls["/w/fresh"] != 0 || e.git.calls["/w/wt"] != 0 {
		t.Errorf("resolve calls = %v", e.git.calls)
	}
	if got := e.str(t, "SELECT git_root FROM dirs WHERE cwd = '/w/alpha'"); got != "/w/alpha" {
		t.Errorf("re-resolved root = %q", got)
	}
}

func TestRawMaxBytes(t *testing.T) {
	long := strings.Repeat("x", 300)
	tests := []struct {
		name          string
		max           int
		storeRaw      bool
		wantRaw       bool
		wantTruncated int
	}{
		{"unlimited", 0, true, true, 0},
		{"over the cap", 200, true, false, 1},
		{"under the cap", 4096, true, true, 0},
		{"store_raw off is not truncation", 200, false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.deps.RawMaxBytes = tt.max
			e.deps.StoreRaw = tt.storeRaw
			p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl",
				tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/x", long).JSON())
			run(t, e, p)
			if got := e.str(t, "SELECT text FROM events"); got != long {
				t.Errorf("text changed: %d bytes", len(got))
			}
			raw := e.str(t, "SELECT raw_text(raw) FROM events")
			if (raw != "") != tt.wantRaw {
				t.Errorf("raw = %q, want present=%v", raw, tt.wantRaw)
			}
			if n := e.count(t, "SELECT count(*) FROM events WHERE raw_truncated = 1"); n != tt.wantTruncated {
				t.Errorf("raw_truncated rows = %d, want %d", n, tt.wantTruncated)
			}
		})
	}
}

func TestRegit(t *testing.T) {
	e := newEnv(t)
	s2 := tf.SmallSession2
	s3 := "33333333-3333-4333-8333-333333333333"
	// Session 1 in /w/stale: not a repo at first ingest (cached negative).
	// Session 2 in /w/alpha: a repo that later becomes unresolvable (deleted
	// worktree): the cached positive result must survive.
	// Session 3 in /w/moved: a repo whose remote changed.
	e.git.infos["/w/moved"] = gitinfo.Info{Root: "/w/moved", RemoteURL: "https://github.com/o/old"}
	p := tf.WriteFile(t, e.projects, "-w/"+sid+".jsonl", tf.User(sid, "u1", "2026-10-01T10:00:00Z", "/w/stale", "a").JSON())
	p2 := tf.WriteFile(t, e.projects, "-w/"+s2+".jsonl", tf.User(s2, "u2", "2026-10-01T10:00:00Z", "/w/alpha", "b").JSON())
	p3 := tf.WriteFile(t, e.projects, "-w/"+s3+".jsonl", tf.User(s3, "u3", "2026-10-01T10:00:00Z", "/w/moved", "c").JSON())
	run(t, e, p, p2, p3)
	if got := e.str(t, "SELECT git_root FROM sessions WHERE native_id = '"+sid+"'"); got != "" {
		t.Fatalf("precondition: git_root = %q", got)
	}

	e.git.infos["/w/stale"] = gitinfo.Info{Root: "/w/stale", RemoteURL: "https://github.com/o/stale"}
	delete(e.git.infos, "/w/alpha")
	e.git.infos["/w/moved"] = gitinfo.Info{Root: "/w/moved", RemoteURL: "https://github.com/o/new"}
	e.git.calls = nil
	sum, err := Regit(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Dirs != 3 || sum.Sessions != 2 {
		t.Errorf("summary = %+v, want 3 dirs, 2 sessions updated", sum)
	}
	if e.git.calls["/w/stale"] != 1 || e.git.calls["/w/alpha"] != 1 {
		t.Errorf("git calls = %v (regit must bypass the cache)", e.git.calls)
	}
	checks := map[string]string{
		"SELECT git_remote_url FROM sessions WHERE native_id = '" + sid + "'": "https://github.com/o/stale",
		"SELECT git_remote_url FROM sessions WHERE native_id = '" + s2 + "'":  "https://github.com/o/alpha",
		"SELECT git_remote_url FROM sessions WHERE native_id = '" + s3 + "'":  "https://github.com/o/new",
		"SELECT git_remote_url FROM dirs WHERE cwd = '/w/stale'":              "https://github.com/o/stale",
		"SELECT git_remote_url FROM dirs WHERE cwd = '/w/alpha'":              "https://github.com/o/alpha",
	}
	for q, want := range checks {
		if got := e.str(t, q); got != want {
			t.Errorf("%s = %q, want %q", q, got, want)
		}
	}
	// Idempotent.
	if sum, err := Regit(ctx, e.deps); err != nil || sum.Sessions != 0 {
		t.Errorf("second Regit = %+v %v", sum, err)
	}
}

// TestSessionOwnerSetsAccount: session-owner records (Claude Code 2.1.317)
// set account and organization of the session; neither an event nor a raw
// line reaches the store, whatever store_raw says.
func TestSessionOwnerSetsAccount(t *testing.T) {
	e := newEnv(t)
	s := "44444444-4444-4444-8444-444444444444"
	p := tf.WriteFile(t, e.projects, "-w-delta/"+s+".jsonl", tf.Join(
		tf.SessionOwner(s),
		tf.User(s, "d1", "2026-10-08T10:00:00.000Z", "/w/delta", "hello"),
		tf.SessionOwner(s),
		tf.Assistant(s, "d2", "2026-10-08T10:00:05.000Z", "/w/delta", "hi"),
	))
	sum := run(t, e, p)
	if sum.Events != 2 {
		t.Errorf("events ingested = %d, want 2: %+v", sum.Events, sum)
	}
	if n := e.count(t, `SELECT count(*) FROM events WHERE raw_text(raw) LIKE '%session-owner%' OR raw_text(raw) LIKE '%acc00000%'`); n != 0 {
		t.Errorf("%d events carry session-owner data in raw", n)
	}
	if n := e.count(t, `SELECT count(*) FROM sessions WHERE id = 'claude:`+s+`' AND account_id = '`+tf.Account1+`' AND organization_id = '`+tf.Organization1+`'`); n != 1 {
		t.Errorf("sessions with account = %d, want 1", n)
	}
}

// TestContinuedInLinksSession: continued-in records (Claude Code 2.1.318)
// set the continuation link of the session without stretching its end;
// neither an event nor a raw line reaches the store.
func TestContinuedInLinksSession(t *testing.T) {
	e := newEnv(t)
	s := "55555555-5555-4555-8555-555555555555"
	next := "66666666-6666-4666-8666-666666666666"
	p := tf.WriteFile(t, e.projects, "-w-epsilon/"+s+".jsonl", tf.Join(
		tf.User(s, "e1", "2026-10-09T10:00:00.000Z", "/w/epsilon", "hello"),
		tf.Assistant(s, "e2", "2026-10-09T10:00:05.000Z", "/w/epsilon", "hi"),
		tf.ContinuedIn(s, "2026-10-09T10:00:06.000Z", next),
	))
	sum := run(t, e, p)
	if sum.Events != 2 {
		t.Errorf("events ingested = %d, want 2: %+v", sum.Events, sum)
	}
	if n := e.count(t, `SELECT count(*) FROM events WHERE raw_text(raw) LIKE '%continued-in%' OR raw_text(raw) LIKE '%`+next+`%'`); n != 0 {
		t.Errorf("%d events carry continued-in data in raw", n)
	}
	if n := e.count(t, `SELECT count(*) FROM sessions WHERE id = 'claude:`+s+`' AND continued_in = 'claude:`+next+`'
		AND ended_at = '2026-10-09T10:00:05.000Z'`); n != 1 {
		t.Errorf("linked sessions = %d, want 1", n)
	}
	// The target is not ingested; the link stays and no row is invented.
	if n := e.count(t, `SELECT count(*) FROM sessions WHERE id = 'claude:`+next+`'`); n != 0 {
		t.Errorf("target sessions = %d, want 0", n)
	}
}

// TestReread: with Reread every source is read from the beginning. Events
// and the full-text index stay unique, and session fields that earlier
// versions did not store (here the account, simulated by clearing it) are
// filled in.
func TestReread(t *testing.T) {
	e := newEnv(t)
	tf.Small(t, e.dir)
	first, err := All(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an archive written before the account was stored.
	db, err := sql.Open("sqlite", "file:"+e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE sessions SET account_id = NULL, organization_id = NULL WHERE id = 'claude:"+sid+"'"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	counts := func() [3]int {
		return [3]int{e.count(t, "SELECT count(*) FROM events"), e.count(t, "SELECT count(*) FROM events_fts"),
			e.count(t, "SELECT count(*) FROM sessions")}
	}
	before := counts()
	if before[0] != first.Events || before[1] != first.Events {
		t.Fatalf("counts after first ingest = %v, summary %d", before, first.Events)
	}
	e.deps.Reread = true
	for i := range 2 {
		sum, err := All(ctx, e.deps)
		if err != nil {
			t.Fatal(err)
		}
		if sum.Events != 0 || sum.Changed != 4 {
			t.Errorf("reread %d summary = %+v, want 0 new events, 4 changed", i+1, sum)
		}
		if got := counts(); got != before {
			t.Errorf("reread %d counts = %v, want %v", i+1, got, before)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM sessions WHERE id = 'claude:`+sid+`' AND account_id = '`+tf.Account1+`'`); n != 1 {
		t.Errorf("account after reread: %d sessions, want 1", n)
	}
	if got := e.count(t, `SELECT count(*) FROM sources WHERE "offset" = 0`); got != 0 {
		t.Errorf("sources left at offset 0: %d", got)
	}
}

// TestRereadRewriteRaw: --reread --rewrite-raw replaces raw of existing
// events with the freshly redacted line and touches nothing else (#130).
func TestRereadRewriteRaw(t *testing.T) {
	e := newEnv(t)
	tf.Small(t, e.dir)
	r, err := redact.New(redact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.deps.Redactor = r
	first, err := All(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an archive written by the version that masked whole values.
	db, err := sql.Open("sqlite", "file:"+e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE events SET raw = '{"text":"«redacted:high_entropy»"}', actor = 'peer' WHERE id = 'claude:a4'`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	counts := func() [3]int {
		return [3]int{e.count(t, "SELECT count(*) FROM events"), e.count(t, "SELECT count(*) FROM events_fts"),
			e.count(t, "SELECT count(*) FROM sessions")}
	}
	before := counts()
	e.deps.Reread, e.deps.RewriteRaw = true, true
	sum, err := All(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Events != 0 || sum.Meta != 0 || sum.RawRewritten != first.Events+first.Meta || sum.Changed != 4 {
		t.Errorf("summary = %+v, want 0 new events, %d raw rewritten, 4 changed", sum, first.Events+first.Meta)
	}
	if got := counts(); got != before {
		t.Errorf("counts = %v, want %v", got, before)
	}
	raw := e.str(t, "SELECT raw_text(raw) FROM events WHERE id = 'claude:a4'")
	if !strings.Contains(raw, "thanks, now count the lines") || strings.Contains(raw, "high_entropy") {
		t.Errorf("raw after rewrite = %q", raw)
	}
	if actor := e.str(t, "SELECT actor FROM events WHERE id = 'claude:a4'"); actor != "peer" {
		t.Errorf("actor changed to %q", actor)
	}
	if got := e.count(t, `SELECT count(*) FROM events WHERE raw IS NULL`); got != 0 {
		t.Errorf("events without raw: %d", got)
	}
	// Without Reread nothing is read, so nothing is rewritten.
	e.deps.Reread = false
	if sum, err := All(ctx, e.deps); err != nil || sum.RawRewritten != 0 || sum.Changed != 0 {
		t.Errorf("without reread: %+v, %v", sum, err)
	}
}

// TestHistorySuppressionIsNotStored: history-suppression records are
// skipped by the adapter, so neither an event nor a raw line reaches the
// store; the session itself ingests as before.
func TestHistorySuppressionIsNotStored(t *testing.T) {
	e := newEnv(t)
	s := "77777777-7777-4777-8777-777777777777"
	p := tf.WriteFile(t, e.projects, "-w-zeta/"+s+".jsonl", tf.Join(
		tf.User(s, "z1", "2026-10-10T10:00:00.000Z", "/w/zeta", "hello"),
		tf.HistorySuppression(s, "2026-10-10T10:00:02.000Z"),
		tf.Assistant(s, "z2", "2026-10-10T10:00:05.000Z", "/w/zeta", "hi"),
	))
	sum := run(t, e, p)
	if sum.Events != 2 {
		t.Errorf("events ingested = %d, want 2: %+v", sum.Events, sum)
	}
	if n := e.count(t, `SELECT count(*) FROM events WHERE raw_text(raw) LIKE '%history-suppression%' OR raw_text(raw) LIKE '%chokepoint%'`); n != 0 {
		t.Errorf("%d events carry history-suppression data in raw", n)
	}
	if n := e.count(t, `SELECT count(*) FROM sessions WHERE id = 'claude:`+s+`'`); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
}

// imageLine is a user record that pastes a small synthetic image.
func imageLine(uuid, ts string, img []byte) tf.Line {
	l := tf.User(sid, uuid, ts, "/w/x", "")
	l["message"] = map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "see the picture"},
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(img)}},
	}}
	return l
}

// TestAttachmentsAreStoredOnce: the data of pasted images leaves raw and
// is kept once per hash (#181); the references survive redaction; with
// StoreBlobs off nothing but the reference is kept.
func TestAttachmentsAreStoredOnce(t *testing.T) {
	img := []byte("\x89PNG\r\n\x1a\nsynthetic picture for the ingest test")
	for _, storeBlobs := range []bool{true, false} {
		t.Run(fmt.Sprintf("store_blobs=%v", storeBlobs), func(t *testing.T) {
			e := newEnv(t)
			r, err := redact.New(redact.Options{})
			if err != nil {
				t.Fatal(err)
			}
			e.deps.Redactor, e.deps.StoreBlobs = r, storeBlobs
			p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl",
				imageLine("u1", "2026-10-01T10:00:00Z", img).JSON()+imageLine("u2", "2026-10-01T10:00:01Z", img).JSON())
			sum := run(t, e, p)
			if sum.Events != 4 {
				t.Errorf("events = %d, want 4 (text and image per line)", sum.Events)
			}
			h := blobref.Hash(img)
			raw := e.str(t, "SELECT raw_text(raw) FROM events WHERE id = 'claude:u1#1'")
			if !strings.Contains(raw, `"data":"blob:sha256:`+h+`"`) || strings.Contains(raw, base64.StdEncoding.EncodeToString(img)) {
				t.Errorf("raw = %q", raw)
			}
			if got := e.str(t, "SELECT text FROM events WHERE id = 'claude:u2#1'"); got != "[image image/png 45 B]" {
				t.Errorf("placeholder = %q", got)
			}
			want := map[string]int{"SELECT count(*) FROM blobs": 1, "SELECT count(*) FROM event_blobs": 2, "SELECT count(*) FROM event_blobs WHERE sha256 = '" + h + "'": 2}
			if !storeBlobs {
				want = map[string]int{"SELECT count(*) FROM blobs": 0, "SELECT count(*) FROM event_blobs": 0}
			}
			for q, n := range want {
				if got := e.count(t, q); got != n {
					t.Errorf("%s = %d, want %d", q, got, n)
				}
			}
			if sum.Blobs != len(want)-2 || (storeBlobs && sum.BlobBytes != int64(len(img))) {
				t.Errorf("summary blobs = %d (%d bytes)", sum.Blobs, sum.BlobBytes)
			}
			if storeBlobs {
				line, err := e.st.RawLine(ctx, "claude:u1#1")
				if err != nil || line != strings.TrimSuffix(imageLine("u1", "2026-10-01T10:00:00Z", img).JSON(), "\n") {
					t.Errorf("RawLine = %q, %v", line, err)
				}
			}
		})
	}
}

// TestRewriteRawFillsBlobsAndMissingRaw: `ingest --all --reread
// --rewrite-raw` brings an archive from before #180/#181 up to date:
// events without raw get the line, lines with attachments get their
// blobs; a second run changes nothing more; a source whose transcript is
// gone keeps what it has.
func TestRewriteRawFillsBlobsAndMissingRaw(t *testing.T) {
	img := []byte("\x89PNG\r\n\x1a\nanother synthetic picture")
	e := newEnv(t)
	e.deps.StoreBlobs = true
	tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl",
		tf.User(sid, "u0", "2026-10-01T09:59:00Z", "/w/x", "hello").JSON()+imageLine("u1", "2026-10-01T10:00:00Z", img).JSON())
	gone := tf.WriteFile(t, e.projects, "-w-y/"+tf.SmallSession2+".jsonl",
		tf.User(tf.SmallSession2, "g1", "2026-10-02T10:00:00Z", "/w/y", "I stay as I am").JSON())
	if sum, err := All(ctx, e.deps); err != nil || sum.Events != 4 {
		t.Fatalf("first run: %+v, %v", sum, err)
	}
	// Simulate the old archive: no raw for u0 (dropped for its size), an
	// extract without the image event for u1, no blobs.
	db, err := sql.Open("sqlite", "file:"+e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE events SET raw = NULL, raw_truncated = 1 WHERE id = 'claude:u0'`,
		`DELETE FROM event_blobs`, `DELETE FROM blobs`,
		`DELETE FROM events WHERE id = 'claude:u1#1'`,
		`UPDATE events SET raw = NULL WHERE id = 'claude:g1'`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_ = db.Close()
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	e.deps.Reread, e.deps.RewriteRaw = true, true
	sum, err := All(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Events != 1 || sum.RawRewritten != 2 || sum.Blobs != 1 || sum.Changed != 1 {
		t.Errorf("rewrite run = %+v, want 1 new event, 2 raw rewritten, 1 blob, 1 changed", sum)
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM events WHERE raw IS NULL":                                                1, // g1: transcript gone
		"SELECT count(*) FROM events WHERE id = 'claude:u0' AND raw IS NOT NULL AND raw_truncated = 0": 1,
		"SELECT count(*) FROM events WHERE id = 'claude:u1#1'":                                         1,
		"SELECT count(*) FROM blobs":                                                                   1,
		"SELECT count(*) FROM event_blobs WHERE event_id = 'claude:u1#1'":                              1,
		"SELECT count(*) FROM events":                                                                  4,
	} {
		if got := e.count(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	// A second run is idempotent: the same rewrites, no new rows.
	again, err := All(ctx, e.deps)
	if err != nil || again.Events != 0 || again.RawRewritten != 3 || again.Blobs != 0 || e.count(t, "SELECT count(*) FROM event_blobs") != 1 {
		t.Errorf("second run = %+v, %v", again, err)
	}
}

// TestRewriteRawCancelKeepsOffsetsConsistent: a cancelled rewrite leaves
// whole lines written and the run can be repeated.
func TestRewriteRawCancelKeepsOffsetsConsistent(t *testing.T) {
	e := newEnv(t)
	var lines []tf.Line
	for i := 0; i < 30; i++ {
		lines = append(lines, tf.User(sid, fmt.Sprintf("u%02d", i), fmt.Sprintf("2026-10-01T10:00:%02dZ", i), "/w/x", "line"))
	}
	p := tf.WriteFile(t, e.projects, "-w-x/"+sid+".jsonl", tf.Join(lines...))
	run(t, e, p)
	e.deps.BatchSize = 5
	e.deps.Reread, e.deps.RewriteRaw = true, true
	cctx, cancel := context.WithCancel(ctx)
	n := 0
	e.deps.Logf = func(string, ...any) {}
	// Cancel after the first batches were written: wrap the store? Simpler:
	// cancel from a goroutine is racy, so cancel through the git resolver,
	// which is called per record with a cwd.
	e.deps.GitResolve = func(c context.Context, cwd string) (gitinfo.Info, error) {
		n++
		if n == 12 {
			cancel()
		}
		return gitinfo.Info{}, gitinfo.ErrNotRepo
	}
	e.deps.ResolveGit = true
	// The dirs cache answers after the first call; force fresh lookups.
	e.git.infos = map[string]gitinfo.Info{}
	_, err := Run(cctx, e.deps, []string{p})
	if err == nil {
		// Cancellation may land after the file was done when the cache
		// short-circuits the resolver; the run is then simply complete.
		t.Log("run completed before the cancel")
	}
	sum := run(t, e, p)
	if sum.RawRewritten != 30 || sum.Events != 0 || e.count(t, "SELECT count(*) FROM events") != 30 {
		t.Errorf("after repeat = %+v", sum)
	}
}

// TestMetaRecords: meta records (attachments, modes, pull-request links)
// land in session_meta redacted and compressed, the same content once per
// session, no event and no full-text hit; store_raw = false keeps none,
// raw_max_bytes drops an oversized one, and a reread or a second
// incremental run adds nothing.
func TestMetaRecords(t *testing.T) {
	secret := "ghp_" + strings.Repeat("aB3", 12)
	s := tf.SmallSession1
	lines := tf.Join(
		tf.PermissionMode(s, "auto"),
		tf.User(s, "u1", "2026-10-01T10:00:00Z", "/w/x", "hello"),
		tf.Attachment(s, "att-1", "2026-10-01T10:00:01Z", "/w/x", "instructions", "token: "+secret),
		tf.Attachment(s, "att-2", "2026-10-01T10:00:01Z", "/w/x", "total_tokens_reminder", "skipped subtype"),
		tf.PRLink(s, "2026-10-01T10:00:02Z", 7),
		tf.Assistant(s, "u2", "2026-10-01T10:00:03Z", "/w/x", "hi"),
		tf.PermissionMode(s, "auto"),
		tf.PRLink(s, "2026-10-01T10:00:04Z", 7),
	)
	t.Run("stored once per content", func(t *testing.T) {
		e := newEnv(t)
		r, err := redact.New(redact.Options{})
		if err != nil {
			t.Fatal(err)
		}
		e.deps.Redactor = r
		p := tf.WriteFile(t, e.projects, "-w-x/"+s+".jsonl", lines)
		sum := run(t, e, p)
		if sum.Events != 2 || sum.Meta != 3 {
			t.Errorf("summary = %+v, want 2 events, 3 meta records", sum)
		}
		checks := map[string]int{
			"SELECT count(*) FROM session_meta": 3,
			"SELECT count(*) FROM session_meta WHERE type = 'permission-mode' AND ts = '2026-10-01T10:00:03.000Z'": 0,
			"SELECT count(*) FROM session_meta WHERE type = 'permission-mode' AND ts IS NULL":                      1,
			"SELECT count(*) FROM session_meta WHERE type = 'pr-link' AND ts = '2026-10-01T10:00:02.000Z'":         1,
			"SELECT count(*) FROM session_meta WHERE id = 'claude:att-1' AND raw_text(raw) LIKE '%«redacted:%'":    1,
			"SELECT count(*) FROM session_meta WHERE raw_text(raw) LIKE '%" + secret + "%'":                        0,
			"SELECT count(*) FROM session_meta WHERE raw_text(raw) LIKE '%total_tokens_reminder%'":                 0,
			"SELECT count(*) FROM events":                                                                           2,
			"SELECT count(*) FROM events_fts WHERE events_fts MATCH 'instructions'":                                 0,
			"SELECT count(*) FROM sessions WHERE id = 'claude:" + s + "' AND ended_at = '2026-10-01T10:00:03.000Z'": 1,
		}
		for q, want := range checks {
			if got := e.count(t, q); got != want {
				t.Errorf("%s = %d, want %d", q, got, want)
			}
		}
		// The same lines again, incrementally: the mode is seen again but
		// not stored again; a new pull request is.
		tf.AppendFile(t, p, tf.Join(tf.PermissionMode(s, "auto"), tf.PRLink(s, "2026-10-01T11:00:00Z", 8)))
		if sum := run(t, e, p); sum.Meta != 1 || sum.Events != 0 {
			t.Errorf("incremental summary = %+v, want 1 meta record", sum)
		}
		e.deps.Reread = true
		if sum := run(t, e, p); sum.Meta != 0 || sum.Events != 0 {
			t.Errorf("reread summary = %+v, want nothing new", sum)
		}
		if got := e.count(t, "SELECT count(*) FROM session_meta"); got != 4 {
			t.Errorf("meta rows = %d, want 4", got)
		}
	})
	t.Run("system prompts and shapes are blobs once across sessions", func(t *testing.T) {
		for _, storeBlobs := range []bool{true, false} {
			t.Run(fmt.Sprintf("store_blobs=%v", storeBlobs), func(t *testing.T) {
				e := newEnv(t)
				r, err := redact.New(redact.Options{})
				if err != nil {
					t.Fatal(err)
				}
				e.deps.Redactor, e.deps.StoreBlobs = r, storeBlobs
				s2 := tf.SmallSession2
				system := []string{"You are a demo.", "token: " + secret}
				tf.WriteFile(t, e.projects, "-w-x/"+s+".jsonl", tf.Join(
					tf.APIRequestShape(s, "2026-10-01T10:00:00Z", system, "Read", "Edit"),
					tf.PromptSnapshot(s, "ps-1", "2026-10-01T10:00:01Z", "/w/x", system...),
					tf.APIRequestShape(s, "2026-10-01T10:05:00Z", system, "Read", "Edit"), // same shape, next turn
				))
				tf.WriteFile(t, e.projects, "-w-y/"+s2+".jsonl", tf.Join(
					tf.APIRequestShape(s2, "2026-10-02T10:00:00Z", system, "Read", "Edit"),
					tf.PromptSnapshot(s2, "ps-2", "2026-10-02T10:00:01Z", "/w/y", system...),
				))
				sum, err := All(ctx, e.deps)
				if err != nil {
					t.Fatal(err)
				}
				// Two distinct values (the shape, the prompt), both kept even
				// without store_blobs: they are redacted JSON, not images.
				if sum.Meta != 4 || sum.Blobs != 2 {
					t.Errorf("summary = %+v, want 4 meta records, 2 blobs", sum)
				}
				for q, want := range map[string]int{
					"SELECT count(*) FROM blobs":                                                    2,
					"SELECT count(*) FROM blobs WHERE media_type = 'application/json'":              2,
					"SELECT count(*) FROM meta_blobs":                                               4,
					"SELECT count(DISTINCT sha256) FROM meta_blobs":                                 2,
					"SELECT count(*) FROM session_meta WHERE type = 'api-request-shape'":            2,
					"SELECT count(*) FROM session_meta WHERE raw_text(raw) LIKE '%blob:sha256:%'":   4,
					"SELECT count(*) FROM session_meta WHERE raw_text(raw) LIKE '%You are a demo%'": 0,
					"SELECT count(*) FROM events":                                                   0,
				} {
					if got := e.count(t, q); got != want {
						t.Errorf("%s = %d, want %d", q, got, want)
					}
				}
				// The blob is redacted and the line refers to the redacted hash.
				sha := e.str(t, "SELECT sha256 FROM meta_blobs WHERE meta_id = 'claude:ps-1'")
				_, data, err := e.st.Blob(ctx, sha)
				if err != nil || !strings.Contains(string(data), "You are a demo") || strings.Contains(string(data), secret) || !strings.Contains(string(data), "«redacted:") {
					t.Errorf("blob = %q, %v", data, err)
				}
				if sha != blobref.Hash(data) {
					t.Errorf("link %s is not the hash of the stored data", sha)
				}
				if raw := e.str(t, "SELECT raw_text(raw) FROM session_meta WHERE id = 'claude:ps-1'"); !strings.Contains(raw, `"systemPrompt":"blob:sha256:`+sha+`"`) {
					t.Errorf("raw = %s", raw)
				}
				// Reread with rewrite: idempotent (the repeated shape line
				// rewrites its record a second time, that is all).
				e.deps.Reread, e.deps.RewriteRaw = true, true
				if again, err := All(ctx, e.deps); err != nil || again.Meta != 0 || again.Blobs != 0 || again.RawRewritten != 5 || e.count(t, "SELECT count(*) FROM meta_blobs") != 4 {
					t.Errorf("reread = %+v, %v", again, err)
				}
			})
		}
	})
	t.Run("store_raw off keeps none", func(t *testing.T) {
		e := newEnv(t)
		e.deps.StoreRaw = false
		p := tf.WriteFile(t, e.projects, "-w-x/"+s+".jsonl", lines)
		if sum := run(t, e, p); sum.Meta != 0 || sum.Events != 2 {
			t.Errorf("summary = %+v", sum)
		}
		if got := e.count(t, "SELECT count(*) FROM session_meta"); got != 0 {
			t.Errorf("meta rows = %d", got)
		}
	})
	t.Run("raw_max_bytes drops an oversized record", func(t *testing.T) {
		e := newEnv(t)
		e.deps.RawMaxBytes = 300
		var logged []string
		e.deps.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
		p := tf.WriteFile(t, e.projects, "-w-x/"+s+".jsonl", tf.Join(
			tf.Attachment(s, "att-big", "2026-10-01T10:00:01Z", "/w/x", "file", strings.Repeat("x", 400)),
			tf.PRLink(s, "2026-10-01T10:00:02Z", 7),
		))
		if sum := run(t, e, p); sum.Meta != 1 {
			t.Errorf("summary = %+v, want 1 meta record", sum)
		}
		if got := e.count(t, "SELECT count(*) FROM session_meta WHERE type = 'pr-link'"); got != 1 {
			t.Errorf("pr-link rows = %d", got)
		}
		if len(logged) != 1 || !strings.Contains(logged[0], "att-big") || !strings.Contains(logged[0], "raw_max_bytes") {
			t.Errorf("log = %q", logged)
		}
	})
	t.Run("batches flush before a meta record too", func(t *testing.T) {
		e := newEnv(t)
		e.deps.BatchSize = 2
		p := tf.WriteFile(t, e.projects, "-w-x/"+s+".jsonl", tf.Join(
			tf.PRLink(s, "2026-10-01T10:00:00Z", 1), tf.PRLink(s, "2026-10-01T10:00:00Z", 2), tf.PRLink(s, "2026-10-01T10:00:00Z", 3),
			tf.User(s, "u1", "2026-10-01T10:00:00Z", "/w/x", "hello"), tf.PRLink(s, "2026-10-01T10:00:00Z", 4),
		))
		if sum := run(t, e, p); sum.Meta != 4 || sum.Events != 1 {
			t.Errorf("summary = %+v", sum)
		}
		if got := e.count(t, `SELECT "offset" FROM sources`); got == 0 {
			t.Error("offset not recorded")
		}
	})
}
