package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/redact"
	"github.com/seletz/total-recall/internal/store"
)

// A synthetic GitHub token: the redactor masks the ghp_ format.
const memToken = "ghp_" + "aB3dE5fG7hJ9kL1mN3pQ5rS7tU9vW1xY3zA5"

// memEnv is an ingest environment with memory switched on and two
// projects: alpha (memory only; its cwd is known from an archived
// session) and beta (memory and a transcript naming its cwd).
func memEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	rd, err := redact.New(redact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.deps.Redactor = rd
	e.deps.StoreMemory = true
	if err := e.st.WriteBatch(ctx, store.SourceState{}, []model.Session{{ID: "claude:a1", Harness: "claude", NativeID: "a1", Cwd: "/w/alpha"}}, nil); err != nil {
		t.Fatal(err)
	}
	e.memFile(t, "-w-alpha", "MEMORY.md", "# Memory\n- [Deploy](deploy.md)\n", "2026-10-01T08:00:00Z")
	e.memFile(t, "-w-alpha", "deploy.md", "Deploy with token "+memToken+" from the vault.\n", "2026-10-01T09:00:00Z")
	e.memFile(t, "-w-beta", "notes.md", "beta lighthouse notes\n", "2026-10-02T08:00:00Z")
	if err := os.WriteFile(filepath.Join(e.projects, "-w-beta", "b1.jsonl"), []byte(`{"type":"user","cwd":"/w/beta"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) memFile(t *testing.T, key, name, content, mtime string) {
	t.Helper()
	p := filepath.Join(e.projects, key, "memory", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := time.Parse(time.RFC3339, mtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, m, m); err != nil {
		t.Fatal(err)
	}
}

func memory(t *testing.T, e *env) Summary {
	t.Helper()
	sum, err := Memory(ctx, e.deps)
	if err != nil || len(sum.Errors) != 0 {
		t.Fatalf("Memory = %+v, %v", sum, err)
	}
	return sum
}

func TestMemoryFirstScanAndRedaction(t *testing.T) {
	e := memEnv(t)
	sum := memory(t, e)
	if sum.MemoryVersions != 3 || sum.MemoryDeleted != 0 {
		t.Errorf("first scan = %+v", sum)
	}
	ps, err := e.st.MemoryProjects(ctx)
	if err != nil || len(ps) != 2 {
		t.Fatalf("projects = %+v, %v", ps, err)
	}
	// alpha's cwd comes from the archived session and is resolved through
	// git like a session's; beta's from its transcript.
	if ps[0].Key != "-w-alpha" || ps[0].Cwd != "/w/alpha" || ps[0].GitRoot != "/w/alpha" || ps[0].GitRemoteURL != "https://github.com/o/alpha" || ps[0].Files != 2 {
		t.Errorf("alpha = %+v", ps[0])
	}
	if ps[1].Key != "-w-beta" || ps[1].Cwd != "/w/beta" || ps[1].GitRoot != "" {
		t.Errorf("beta = %+v", ps[1])
	}
	// The content is redacted before it is hashed and indexed.
	_, text, err := e.st.MemoryContent(ctx, ps[0].ID, "deploy.md", time.Time{})
	if err != nil || strings.Contains(text, memToken) || !strings.Contains(text, "«redacted:") || !strings.Contains(text, "from the vault") {
		t.Errorf("deploy.md = %q, %v", text, err)
	}
	if hits, _ := e.st.SearchMemory(ctx, memToken, store.MemorySearchOptions{}); len(hits) != 0 {
		t.Errorf("token in the index: %+v", hits)
	}
	if hits, _ := e.st.SearchMemory(ctx, "vault", store.MemorySearchOptions{}); len(hits) != 1 {
		t.Errorf("vault = %+v", hits)
	}
	// Memory is no event, no session, no source.
	if n := e.count(t, "SELECT count(*) FROM events"); n != 0 {
		t.Errorf("events = %d", n)
	}
	if n := e.count(t, "SELECT count(*) FROM sources"); n != 0 {
		t.Errorf("sources = %d", n)
	}
}

func TestMemoryOnlyReadsChangedFiles(t *testing.T) {
	e := memEnv(t)
	memory(t, e)
	// An unchanged file (same size and mtime) is not read: make it
	// unreadable, the scan must not notice.
	p := filepath.Join(e.projects, "-w-alpha", "memory", "MEMORY.md")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	sum := memory(t, e)
	if sum.MemoryVersions != 0 || sum.MemoryDeleted != 0 || sum.MemoryRead != 0 {
		t.Errorf("unchanged scan = %+v", sum)
	}
	// A touched file is read but makes no version; a changed one does.
	e.memFile(t, "-w-beta", "notes.md", "beta lighthouse notes\n", "2026-10-03T08:00:00Z")
	e.memFile(t, "-w-alpha", "deploy.md", "Deploy from the vault only.\n", "2026-10-03T09:00:00Z")
	sum = memory(t, e)
	if sum.MemoryVersions != 1 || sum.MemoryRead != 2 {
		t.Errorf("change scan = %+v", sum)
	}
	id := projectByKey(t, e, "-w-alpha")
	vs, err := e.st.MemoryVersions(ctx, id, "deploy.md")
	if err != nil || len(vs) != 2 || !vs[1].TS.Equal(time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("versions = %+v, %v", vs, err)
	}
}

func projectByKey(t *testing.T, e *env, key string) int64 {
	t.Helper()
	ps, err := e.st.MemoryProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Key == key {
			return p.ID
		}
	}
	t.Fatalf("no project %s", key)
	return 0
}

func TestMemoryDeletions(t *testing.T) {
	e := memEnv(t)
	memory(t, e)
	// A deleted file is a version of its own, once.
	if err := os.Remove(filepath.Join(e.projects, "-w-alpha", "memory", "deploy.md")); err != nil {
		t.Fatal(err)
	}
	if sum := memory(t, e); sum.MemoryDeleted != 1 {
		t.Errorf("delete = %+v", sum)
	}
	if sum := memory(t, e); sum.MemoryDeleted != 0 {
		t.Errorf("second scan after delete = %+v", sum)
	}
	// A project whose memory directory is gone loses all its files.
	if err := os.RemoveAll(filepath.Join(e.projects, "-w-beta", "memory")); err != nil {
		t.Fatal(err)
	}
	if sum := memory(t, e); sum.MemoryDeleted != 1 {
		t.Errorf("project gone = %+v", sum)
	}
	// Without the projects dir nothing is known: no deletions.
	if err := os.Rename(e.projects, e.projects+".away"); err != nil {
		t.Fatal(err)
	}
	if sum := memory(t, e); sum.MemoryDeleted != 0 {
		t.Errorf("projects dir missing = %+v", sum)
	}
	if st, _ := e.st.MemoryStats(ctx); st.Files != 1 || st.Deleted != 2 {
		t.Errorf("stats = %+v", st)
	}
}

func TestAllIncludesMemory(t *testing.T) {
	e := memEnv(t)
	e.deps.StoreMemory = false
	if _, err := All(ctx, e.deps); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "SELECT count(*) FROM memory_versions"); n != 0 {
		t.Errorf("store_memory = false: %d versions", n)
	}
	e.deps.StoreMemory = true
	sum, err := All(ctx, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	if sum.MemoryVersions != 3 {
		t.Errorf("All = %+v", sum)
	}
}

func TestMemoryLocked(t *testing.T) {
	e := memEnv(t)
	unlock, ok, err := lockFile(e.deps.LockDir, "memory:claude")
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer unlock()
	sum := memory(t, e)
	if sum.Locked != 1 || sum.MemoryVersions != 0 {
		t.Errorf("locked = %+v", sum)
	}
}
