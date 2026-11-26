package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// memFile writes a synthetic memory file below the test projects dir
// with the given mtime.
func (e *testEnv) memFile(t *testing.T, key, name, content string, mtime time.Time) {
	t.Helper()
	p := filepath.Join(e.projects, key, "memory", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// memoryEnv has memory in alpha (two files) and beta (an index), ingested.
func memoryEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	d := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	e.memFile(t, "-w-alpha", "MEMORY.md", "# Memory\n- [Lighthouse](lighthouse.md)\n", d)
	e.memFile(t, "-w-alpha", "lighthouse.md", "The beacon of the lighthouse rotates every twelve seconds.\n", d.Add(time.Hour))
	e.memFile(t, "-w-beta", "MEMORY.md", "# Memory of beta\n", d)
	if out := e.mustRun(t, "ingest", "--all"); !strings.Contains(out, "memory: 3 new versions, 0 deleted") {
		t.Fatalf("ingest --all = %q", out)
	}
	return e
}

func TestMemoryList(t *testing.T) {
	t.Parallel()
	e := memoryEnv(t)
	files := decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json"))
	if len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	f := files[0]
	if f.Project != "/w/alpha" || f.ProjectKey != "-w-alpha" || f.Harness != "claude" || f.Cwd != "/w/alpha" || f.GitRoot != "" ||
		f.Path != "MEMORY.md" || f.SHA256 == "" || f.Bytes == 0 || f.Deleted || f.Changed != "2026-10-01T08:00:00.000Z" || f.Versions != 1 {
		t.Errorf("first file = %+v", f)
	}
	if files[2].Project != "/w/beta" {
		t.Errorf("order = %+v", files)
	}
	out := e.mustRun(t, "memory")
	for _, want := range []string{"PROJECT", "FILE", "CHANGED", "/w/alpha", "lighthouse.md", "2026-10-01 11:00"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	// --project: a part of the name, the path or the native key.
	for _, p := range []string{"alpha", "/w/alpha", "-w-alpha"} {
		if files := decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json", "--project", p)); len(files) != 2 {
			t.Errorf("--project %s = %d files", p, len(files))
		}
	}
	r := e.run(t, "", "memory", "--json", "--project", "gamma")
	if r.code == 0 || decode[ErrorJSON](t, r.stdout).Error.Kind != ErrKindNotFound {
		t.Errorf("unknown project = %+v", r)
	}
}

func TestMemoryHistoryAtAndDeleted(t *testing.T) {
	t.Parallel()
	e := memoryEnv(t)
	// A change and a deletion, archived by the next Stop hook.
	e.memFile(t, "-w-alpha", "lighthouse.md", "The beacon rotates every ten seconds now.\n", time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC))
	if err := os.Remove(filepath.Join(e.projects, "-w-beta", "memory", "MEMORY.md")); err != nil {
		t.Fatal(err)
	}
	if r := e.run(t, e.hookPayload(t, "Stop"), "ingest", "--hook"); r.code != 0 || r.stdout != "" {
		t.Fatalf("hook = %+v", r)
	}
	if n := e.count(t, "SELECT count(*) FROM memory_versions"); n != 5 {
		t.Errorf("versions after the hook = %d, want 5", n)
	}

	vs := decode[[]MemoryVersionJSON](t, e.mustRun(t, "memory", "--json", "--history", "lighthouse.md"))
	if len(vs) != 2 || vs[0].Project != "/w/alpha" || vs[0].Path != "lighthouse.md" || vs[0].TS != "2026-10-01T09:00:00.000Z" ||
		vs[1].TS != "2026-10-10T08:00:00.000Z" || vs[1].SHA256 == vs[0].SHA256 || vs[0].Deleted || vs[0].ObservedAt == "" {
		t.Errorf("history = %+v", vs)
	}
	// MEMORY.md is in both projects: --project picks one.
	r := e.run(t, "", "memory", "--json", "--history", "MEMORY.md")
	if r.code == 0 || decode[ErrorJSON](t, r.stdout).Error.Kind != ErrKindAmbiguous || !strings.Contains(r.stderr, "/w/beta") {
		t.Errorf("ambiguous history = %+v", r)
	}
	vs = decode[[]MemoryVersionJSON](t, e.mustRun(t, "memory", "--json", "--history", "MEMORY.md", "--project", "beta"))
	if len(vs) != 2 || !vs[1].Deleted || vs[1].SHA256 != "" {
		t.Errorf("beta history = %+v", vs)
	}
	if r := e.run(t, "", "memory", "--json", "--history", "nope.md"); r.code == 0 || decode[ErrorJSON](t, r.stdout).Error.Kind != ErrKindNotFound {
		t.Errorf("unknown file = %+v", r)
	}
	out := e.mustRun(t, "memory", "--history", "lighthouse.md")
	for _, want := range []string{"CHANGED", "OBSERVED", "2026-10-10 10:00", "lighthouse.md", vs0(t, e)[:12]} {
		if !strings.Contains(out, want) {
			t.Errorf("history table lacks %q:\n%s", want, out)
		}
	}

	// Now: beta's index is gone; --deleted shows it; --at shows the old state.
	if files := decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json")); len(files) != 2 {
		t.Errorf("now = %+v", files)
	}
	files := decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json", "--deleted"))
	if len(files) != 3 || !files[2].Deleted || files[2].SHA256 != "" {
		t.Errorf("--deleted = %+v", files)
	}
	files = decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json", "--at", "2026-10-05"))
	if len(files) != 3 || string(files[1].SHA256) != vs0(t, e) {
		t.Errorf("--at = %+v", files)
	}
	if r := e.run(t, "", "memory", "--at", "yesterday"); r.code == 0 {
		t.Errorf("bad --at = %+v", r)
	}
	if r := e.run(t, "", "memory", "--history", "a.md", "--grep", "x"); r.code == 0 {
		t.Errorf("--history with --grep = %+v", r)
	}
}

// vs0 is the hash of the first version of alpha's lighthouse.md.
func vs0(t *testing.T, e *testEnv) string {
	t.Helper()
	return string(decode[[]MemoryVersionJSON](t, e.mustRun(t, "memory", "--json", "--history", "lighthouse.md"))[0].SHA256)
}

func TestMemoryGrep(t *testing.T) {
	t.Parallel()
	e := memoryEnv(t)
	hits := decode[[]MemoryHitJSON](t, e.mustRun(t, "memory", "--json", "--grep", "beacon"))
	if len(hits) != 1 || hits[0].Project != "/w/alpha" || hits[0].ProjectKey != "-w-alpha" || hits[0].Path != "lighthouse.md" ||
		!strings.Contains(string(hits[0].Snippet), "[beacon]") || hits[0].SHA256 == "" || hits[0].Changed != "2026-10-01T09:00:00.000Z" {
		t.Errorf("grep = %+v", hits)
	}
	if hits := decode[[]MemoryHitJSON](t, e.mustRun(t, "memory", "--json", "--grep", "memory", "--project", "beta")); len(hits) != 1 || hits[0].Project != "/w/beta" {
		t.Errorf("grep in beta = %+v", hits)
	}
	if hits := decode[[]MemoryHitJSON](t, e.mustRun(t, "memory", "--json", "--grep", "nothing-like-this")); len(hits) != 0 {
		t.Errorf("no hits = %+v", hits)
	}
	out := e.mustRun(t, "memory", "--grep", "beacon")
	if !strings.Contains(out, "lighthouse.md") || !strings.Contains(out, "[beacon]") {
		t.Errorf("grep text:\n%s", out)
	}
	// The event search does not see memory.
	if out := e.run(t, "", "search", "beacon"); strings.Contains(out.stdout, "beacon") {
		t.Errorf("search sees memory: %q", out.stdout)
	}
}

func TestMemoryStatusAndBlob(t *testing.T) {
	t.Parallel()
	e := memoryEnv(t)
	s := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if s.MemoryProjects != 2 || s.MemoryFiles != 3 || s.MemoryDeleted != 0 || s.MemoryVersions != 3 || s.MemoryBytes <= 0 || s.MemoryStoredBytes <= 0 {
		t.Errorf("status memory = %+v", s)
	}
	if out := e.mustRun(t, "status"); !strings.Contains(out, "memory") || !strings.Contains(out, "3 files in 2 projects") {
		t.Errorf("status text:\n%s", out)
	}
	files := decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json", "--project", "beta"))
	b := decode[BlobJSON](t, e.mustRun(t, "blob", "--json", string(files[0].SHA256)))
	if b.MediaType != "text/markdown" || len(b.Memory) != 1 || b.Memory[0].Project != "/w/beta" || b.Memory[0].Path != "MEMORY.md" || b.Memory[0].ProjectKey != "-w-beta" {
		t.Errorf("blob --json = %+v", b)
	}
	if out := e.mustRun(t, "blob", string(files[0].SHA256)); out != "# Memory of beta\n" {
		t.Errorf("blob = %q", out)
	}
}

func TestMemorySwitchedOff(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.memFile(t, "-w-alpha", "MEMORY.md", "# Memory\n", time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC))
	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append(cfg, []byte("\n[ingest]\nstore_memory = false\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	e.mustRun(t, "ingest", "--all")
	if r := e.run(t, e.hookPayload(t, "Stop"), "ingest", "--hook"); r.code != 0 {
		t.Fatalf("hook = %+v", r)
	}
	if n := e.count(t, "SELECT count(*) FROM memory_versions"); n != 0 {
		t.Errorf("store_memory = false: %d versions", n)
	}
	if files := decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json")); len(files) != 0 {
		t.Errorf("files = %+v", files)
	}
}

func TestMemoryTimeline(t *testing.T) {
	t.Parallel()
	e := memoryEnv(t)
	e.memFile(t, "-w-alpha", "lighthouse.md", "The beacon rotates every ten seconds now.\n", time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC))
	if err := os.Remove(filepath.Join(e.projects, "-w-beta", "memory", "MEMORY.md")); err != nil {
		t.Fatal(err)
	}
	if r := e.run(t, e.hookPayload(t, "Stop"), "ingest", "--hook"); r.code != 0 {
		t.Fatalf("hook = %+v", r)
	}

	events := decode[[]MemoryEventJSON](t, e.mustRun(t, "memory", "--json", "--timeline"))
	type row struct{ project, path, kind string }
	var got []row
	for _, ev := range events {
		got = append(got, row{ev.Project, ev.Path, ev.Kind})
	}
	want := []row{
		{"/w/beta", "MEMORY.md", "deleted"},
		{"/w/alpha", "lighthouse.md", "changed"},
		{"/w/alpha", "lighthouse.md", "added"},
		{"/w/alpha", "MEMORY.md", "added"},
		{"/w/beta", "MEMORY.md", "added"},
	}
	// Same time: the later sequence first, so beta's index (ingested after
	// alpha's) comes before alpha's.
	want[3], want[4] = want[4], want[3]
	if len(got) != len(want) {
		t.Fatalf("events = %+v", events)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	ch := events[1]
	if ch.ProjectKey != "-w-alpha" || ch.Harness != "claude" || ch.TS != "2026-10-10T08:00:00.000Z" || ch.PrevTS != "2026-10-01T09:00:00.000Z" ||
		ch.PrevSHA256 == "" || ch.SHA256 == "" || ch.SHA256 == ch.PrevSHA256 || ch.ObservedAt == "" || ch.Bytes == 0 || ch.DeltaBytes != ch.Bytes-events[2].Bytes {
		t.Errorf("changed event = %+v", ch)
	}
	if d := events[0]; d.SHA256 != "" || d.PrevSHA256 != "" || d.Bytes != 0 || d.DeltaBytes >= 0 {
		t.Errorf("deleted event = %+v", d)
	}
	if a := events[2]; a.PrevTS != "" || a.PrevSHA256 != "" || a.DeltaBytes != a.Bytes {
		t.Errorf("added event = %+v", a)
	}

	// Sum equals parts: the events of a file are its versions, and their
	// size changes add up to its size now (0 once deleted).
	files := decode[[]MemoryFileJSON](t, e.mustRun(t, "memory", "--json", "--deleted"))
	type id struct{ project, path string }
	count, sum := map[id]int{}, map[id]int64{}
	for _, ev := range events {
		count[id{ev.ProjectKey, ev.Path}]++
		sum[id{ev.ProjectKey, ev.Path}] += ev.DeltaBytes
	}
	if len(count) != len(files) {
		t.Errorf("%d files in the timeline, %d in memory --deleted", len(count), len(files))
	}
	for _, f := range files {
		k := id{f.ProjectKey, f.Path}
		if count[k] != f.Versions || sum[k] != f.Bytes {
			t.Errorf("%v: %d events, delta sum %d; want %d versions, %d bytes", k, count[k], sum[k], f.Versions, f.Bytes)
		}
	}

	if n := len(decode[[]MemoryEventJSON](t, e.mustRun(t, "memory", "--json", "--timeline", "--project", "alpha"))); n != 3 {
		t.Errorf("--project alpha: %d events, want 3", n)
	}
	if n := len(decode[[]MemoryEventJSON](t, e.mustRun(t, "memory", "--json", "--timeline", "--at", "2026-10-05"))); n != 3 {
		t.Errorf("--at: %d events, want 3", n)
	}
	out := e.mustRun(t, "memory", "--timeline")
	for _, want := range []string{"CHANGED", "KIND", "2026-10-10 10:00", "deleted", "lighthouse.md", "/w/beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("timeline table lacks %q:\n%s", want, out)
		}
	}
	for _, args := range [][]string{
		{"memory", "--timeline", "--history", "a.md"},
		{"memory", "--timeline", "--grep", "x"},
		{"memory", "--timeline", "--deleted"},
	} {
		if r := e.run(t, "", args...); r.code == 0 || !strings.Contains(r.stderr, "timeline") {
			t.Errorf("%v = %+v", args, r)
		}
	}
	if r := e.run(t, "", "memory", "--json", "--timeline", "--project", "nope"); r.code == 0 || decode[ErrorJSON](t, r.stdout).Error.Kind != ErrKindNotFound {
		t.Errorf("unknown project = %+v", r)
	}
}
