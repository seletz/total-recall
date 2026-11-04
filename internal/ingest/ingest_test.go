package ingest

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	if sum.Sessions != 3 {
		t.Errorf("sessions touched = %d, want 3", sum.Sessions)
	}
	// The workflow journal yields no session but its offset is recorded.
	if got := e.count(t, `SELECT count(*) FROM sources WHERE path LIKE '%journal.jsonl' AND "offset" > 0`); got != 1 {
		t.Errorf("journal source rows = %d", got)
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
			raw := e.str(t, "SELECT raw FROM events")
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
			raw := e.str(t, "SELECT raw FROM events")
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
	if n := e.count(t, `SELECT count(*) FROM events WHERE raw LIKE '%session-owner%' OR raw LIKE '%acc00000%'`); n != 0 {
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
	if n := e.count(t, `SELECT count(*) FROM events WHERE raw LIKE '%continued-in%' OR raw LIKE '%`+next+`%'`); n != 0 {
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
	if sum.Events != 0 || sum.RawRewritten != first.Events || sum.Changed != 4 {
		t.Errorf("summary = %+v, want 0 new events, %d raw rewritten, 4 changed", sum, first.Events)
	}
	if got := counts(); got != before {
		t.Errorf("counts = %v, want %v", got, before)
	}
	raw := e.str(t, "SELECT raw FROM events WHERE id = 'claude:a4'")
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
