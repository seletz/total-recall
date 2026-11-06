package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
)

// Synthetic memory files of two invented projects.
const (
	memIndex   = "# Memory\n\n- [Build](build.md) - how the kompass build works\n"
	memBuild   = "---\nname: build\n---\nThe kompass build uses mise; run the tests with mise run test.\n"
	memBuild2  = "---\nname: build\n---\nThe kompass build uses mise and a static binary.\n"
	memOther   = "Lighthouse notes: the beacon rotates every twelve seconds.\n"
	memKey     = "-w-kompass"
	memKey2    = "-w-lighthouse"
	memHarness = "claude"
)

func change(path, content string, mtime string) MemoryChange {
	return MemoryChange{Path: path, Size: int64(len(content)), MTime: ts(mtime), Content: []byte(content)}
}

func writeMemory(t *testing.T, s *Store, u MemoryUpdate) MemoryWriteResult {
	t.Helper()
	res, err := s.WriteMemory(ctx, u)
	if err != nil {
		t.Fatalf("WriteMemory: %v", err)
	}
	return res
}

// memoryFixture writes three observations: the first scan (two files of
// kompass, one of lighthouse), a change of build.md, then its deletion.
func memoryFixture(t *testing.T, s *Store) {
	t.Helper()
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Cwd: "/w/kompass", Observed: ts("2026-10-01T10:00:00Z"), Changes: []MemoryChange{
		change("MEMORY.md", memIndex, "2026-09-30T08:00:00Z"),
		change("build.md", memBuild, "2026-09-30T09:00:00Z"),
	}})
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey2, Observed: ts("2026-10-01T10:00:00Z"), Changes: []MemoryChange{
		change("notes.md", memOther, "2026-09-15T08:00:00Z"),
	}})
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-05T10:00:00Z"), Changes: []MemoryChange{
		change("build.md", memBuild2, "2026-10-05T09:30:00Z"),
	}})
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-09T10:00:00Z"), Changes: []MemoryChange{
		{Path: "build.md", Deleted: true},
	}})
}

func projectID(t *testing.T, s *Store, key string) int64 {
	t.Helper()
	ps, err := s.MemoryProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Key == key {
			return p.ID
		}
	}
	t.Fatalf("no memory project %q in %+v", key, ps)
	return 0
}

func TestWriteMemoryVersions(t *testing.T) {
	s, _ := openTestStore(t)
	res := writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Cwd: "/w/kompass", Observed: ts("2026-10-01T10:00:00Z"), Changes: []MemoryChange{
		change("MEMORY.md", memIndex, "2026-09-30T08:00:00Z"),
		change("build.md", memBuild, "2026-09-30T09:00:00Z"),
	}})
	if res.Versions != 2 || res.Blobs != 2 || res.BlobBytes != int64(len(memIndex)+len(memBuild)) || res.Deleted != 0 || res.Touched != 0 {
		t.Errorf("first scan = %+v", res)
	}
	// Same content again, newer mtime (touched): no version, no blob, the
	// stored mtime follows so the next scan skips the file.
	res = writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-02T10:00:00Z"), Changes: []MemoryChange{
		change("build.md", memBuild, "2026-10-02T09:00:00Z"),
	}})
	if res.Versions != 0 || res.Blobs != 0 || res.Touched != 1 {
		t.Errorf("touch = %+v", res)
	}
	state, err := s.MemoryState(ctx, memHarness)
	if err != nil {
		t.Fatal(err)
	}
	ps := state[memKey]
	if ps.Cwd != "/w/kompass" || len(ps.Files) != 2 || !ps.Files["build.md"].MTime.Equal(ts("2026-10-02T09:00:00Z")) ||
		ps.Files["build.md"].SHA256 != blobref.Hash([]byte(memBuild)) || ps.Files["build.md"].Size != int64(len(memBuild)) {
		t.Errorf("state = %+v", ps)
	}
	// A change makes a version with the file's mtime as its time.
	res = writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-05T10:00:00Z"), Changes: []MemoryChange{
		change("build.md", memBuild2, "2026-10-05T09:30:00Z"),
	}})
	if res.Versions != 1 || res.Blobs != 1 {
		t.Errorf("change = %+v", res)
	}
	// A deletion is a version of its own; deleting twice records it once.
	for range 2 {
		res = writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-09T10:00:00Z"), Changes: []MemoryChange{{Path: "build.md", Deleted: true}}})
	}
	if res.Deleted != 0 {
		t.Errorf("second deletion = %+v, want nothing", res)
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM memory_projects":                                                1,
		"SELECT count(*) FROM memory_files":                                                   2,
		"SELECT count(*) FROM memory_files WHERE path = 'build.md' AND sha256 IS NULL":        1,
		"SELECT count(*) FROM memory_versions":                                                4,
		"SELECT count(*) FROM memory_versions WHERE sha256 IS NULL AND ts = observed_at":      1,
		"SELECT count(*) FROM memory_versions WHERE ts = '2026-10-05T09:30:00.000Z'":          1,
		"SELECT count(*) FROM blobs WHERE media_type = 'text/markdown' AND encoding = 'zstd'": 3,
		"SELECT count(*) FROM memory_fts":                                                     1, // the deleted file left the index
		"SELECT count(*) FROM events_fts":                                                     0,
		"SELECT count(*) FROM events":                                                         0,
		"SELECT count(*) FROM sessions":                                                       0,
	} {
		if got := countRows(t, s, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	// A file that comes back gets a new version and is searchable again.
	res = writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-10T10:00:00Z"), Changes: []MemoryChange{
		change("build.md", memBuild, "2026-10-10T09:00:00Z"),
	}})
	if res.Versions != 1 || res.Blobs != 0 {
		t.Errorf("recreate = %+v (blob known already)", res)
	}
	if n := countRows(t, s, "SELECT count(*) FROM memory_fts WHERE memory_fts MATCH 'tests'"); n != 1 {
		t.Errorf("recreated file in index = %d", n)
	}
}

func TestWriteMemoryRefusals(t *testing.T) {
	s, _ := openTestStore(t)
	for name, u := range map[string]MemoryUpdate{
		"no harness":  {Key: memKey, Changes: []MemoryChange{change("a.md", "x", "2026-10-01T10:00:00Z")}},
		"no key":      {Harness: memHarness, Changes: []MemoryChange{change("a.md", "x", "2026-10-01T10:00:00Z")}},
		"no path":     {Harness: memHarness, Key: memKey, Changes: []MemoryChange{change("", "x", "2026-10-01T10:00:00Z")}},
		"absolute":    {Harness: memHarness, Key: memKey, Changes: []MemoryChange{change("/etc/a.md", "x", "2026-10-01T10:00:00Z")}},
		"parent path": {Harness: memHarness, Key: memKey, Changes: []MemoryChange{change("../a.md", "x", "2026-10-01T10:00:00Z")}},
	} {
		if _, err := s.WriteMemory(ctx, u); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if n := countRows(t, s, "SELECT count(*) FROM memory_projects"); n != 0 {
		t.Errorf("refused updates left %d projects", n)
	}
}

func TestMemoryProjectsAndFiles(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.PutDir(ctx, DirInfo{Cwd: "/w/kompass", GitRoot: "/w/kompass", GitRemoteURL: "https://example.com/mara/kompass"}); err != nil {
		t.Fatal(err)
	}
	memoryFixture(t, s)
	ps, err := s.MemoryProjects(ctx)
	if err != nil || len(ps) != 2 {
		t.Fatalf("MemoryProjects = %+v, %v", ps, err)
	}
	k, l := ps[0], ps[1]
	if k.Key != memKey || k.Harness != memHarness || k.Cwd != "/w/kompass" || k.GitRoot != "/w/kompass" || k.GitRemoteURL != "https://example.com/mara/kompass" ||
		k.Files != 1 || k.Deleted != 1 || k.Versions != 4 || !k.LastChange.Equal(ts("2026-10-09T10:00:00Z")) {
		t.Errorf("kompass = %+v", k)
	}
	if l.Key != memKey2 || l.Cwd != "" || l.Files != 1 || l.Versions != 1 {
		t.Errorf("lighthouse = %+v", l)
	}

	// Now: only what exists; with Deleted also the deleted file.
	files, err := s.MemoryFiles(ctx, MemoryFilter{ProjectIDs: []int64{k.ID}})
	if err != nil || len(files) != 1 || files[0].Path != "MEMORY.md" || files[0].Bytes != int64(len(memIndex)) || files[0].Versions != 1 ||
		!files[0].Changed.Equal(ts("2026-09-30T08:00:00Z")) || files[0].SHA256 != blobref.Hash([]byte(memIndex)) {
		t.Errorf("files now = %+v, %v", files, err)
	}
	files, _ = s.MemoryFiles(ctx, MemoryFilter{ProjectIDs: []int64{k.ID}, Deleted: true})
	if len(files) != 2 || files[1].Path != "build.md" || !files[1].Deleted || files[1].SHA256 != "" || files[1].Versions != 3 {
		t.Errorf("files with deleted = %+v", files)
	}
	// At a point in time: the version valid then.
	for _, c := range []struct {
		at   string
		want map[string]string // path -> content hash
	}{
		{"2026-09-01T00:00:00Z", map[string]string{}},
		{"2026-09-20T00:00:00Z", map[string]string{"notes.md": memOther}},
		{"2026-10-01T00:00:00Z", map[string]string{"MEMORY.md": memIndex, "build.md": memBuild, "notes.md": memOther}},
		{"2026-10-06T00:00:00Z", map[string]string{"MEMORY.md": memIndex, "build.md": memBuild2, "notes.md": memOther}},
		{"2026-10-10T00:00:00Z", map[string]string{"MEMORY.md": memIndex, "notes.md": memOther}},
	} {
		files, err := s.MemoryFiles(ctx, MemoryFilter{At: ts(c.at)})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, f := range files {
			got[f.Path] = f.SHA256
		}
		if len(got) != len(c.want) {
			t.Errorf("at %s: %v, want %d files", c.at, got, len(c.want))
		}
		for p, content := range c.want {
			if got[p] != blobref.Hash([]byte(content)) {
				t.Errorf("at %s: %s = %s", c.at, p, got[p])
			}
		}
	}
	// Filter by project.
	files, _ = s.MemoryFiles(ctx, MemoryFilter{ProjectIDs: []int64{l.ID}})
	if len(files) != 1 || files[0].Path != "notes.md" || files[0].ProjectID != l.ID {
		t.Errorf("lighthouse files = %+v", files)
	}
}

func TestMemoryVersionsAndContent(t *testing.T) {
	s, _ := openTestStore(t)
	memoryFixture(t, s)
	id := projectID(t, s, memKey)
	vs, err := s.MemoryVersions(ctx, id, "build.md")
	if err != nil || len(vs) != 3 {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	if vs[0].SHA256 != blobref.Hash([]byte(memBuild)) || !vs[0].TS.Equal(ts("2026-09-30T09:00:00Z")) || !vs[0].Observed.Equal(ts("2026-10-01T10:00:00Z")) ||
		vs[0].Bytes != int64(len(memBuild)) || vs[0].Deleted {
		t.Errorf("v1 = %+v", vs[0])
	}
	if vs[1].SHA256 != blobref.Hash([]byte(memBuild2)) || !vs[2].Deleted || vs[2].SHA256 != "" || !vs[2].TS.Equal(ts("2026-10-09T10:00:00Z")) {
		t.Errorf("v2, v3 = %+v, %+v", vs[1], vs[2])
	}
	if _, err := s.MemoryVersions(ctx, id, "nope.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown file: %v", err)
	}

	for _, c := range []struct {
		at      time.Time
		want    string
		missing bool
	}{
		{at: time.Time{}, missing: true}, // now: deleted
		{at: ts("2026-10-01T00:00:00Z"), want: memBuild},
		{at: ts("2026-10-05T09:30:00Z"), want: memBuild2},
		{at: ts("2026-09-01T00:00:00Z"), missing: true}, // before the first version
	} {
		v, text, err := s.MemoryContent(ctx, id, "build.md", c.at)
		if c.missing {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("at %v: %q, %v, want ErrNotFound", c.at, text, err)
			}
			continue
		}
		if err != nil || text != c.want || v.SHA256 != blobref.Hash([]byte(c.want)) {
			t.Errorf("at %v: %q (%+v), %v", c.at, text, v, err)
		}
	}
	if _, text, err := s.MemoryContent(ctx, id, "MEMORY.md", time.Time{}); err != nil || text != memIndex {
		t.Errorf("current MEMORY.md = %q, %v", text, err)
	}
}

func TestSearchMemory(t *testing.T) {
	s, _ := openTestStore(t)
	memoryFixture(t, s)
	k, l := projectID(t, s, memKey), projectID(t, s, memKey2)
	// Only the current version of an existing file is indexed: "mise" was
	// in build.md, which is deleted now; MEMORY.md mentions kompass.
	hits, err := s.SearchMemory(ctx, "kompass", MemorySearchOptions{})
	if err != nil || len(hits) != 1 || hits[0].Path != "MEMORY.md" || hits[0].ProjectID != k || !strings.Contains(hits[0].Snippet, "[kompass]") ||
		hits[0].SHA256 != blobref.Hash([]byte(memIndex)) {
		t.Errorf("kompass = %+v, %v", hits, err)
	}
	if hits, _ := s.SearchMemory(ctx, "mise", MemorySearchOptions{}); len(hits) != 0 {
		t.Errorf("deleted content found: %+v", hits)
	}
	// Diacritics and case fold like the event search.
	if hits, _ := s.SearchMemory(ctx, "BEACON", MemorySearchOptions{}); len(hits) != 1 || hits[0].ProjectID != l {
		t.Errorf("BEACON = %+v", hits)
	}
	// Project filter, highlight, limit.
	if hits, _ := s.SearchMemory(ctx, "beacon", MemorySearchOptions{ProjectIDs: []int64{k}}); len(hits) != 0 {
		t.Errorf("beacon in kompass = %+v", hits)
	}
	hits, _ = s.SearchMemory(ctx, "beacon", MemorySearchOptions{ProjectIDs: []int64{l}, HighlightStart: "<", HighlightEnd: ">", Limit: 5})
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, "<beacon>") {
		t.Errorf("highlight = %+v", hits)
	}
	// User input is quoted unless Raw; raw FTS syntax works with Raw.
	if _, err := s.SearchMemory(ctx, `"unbalanced AND (`, MemorySearchOptions{}); err != nil {
		t.Errorf("quoted input: %v", err)
	}
	if hits, err := s.SearchMemory(ctx, "beacon OR kompass", MemorySearchOptions{Raw: true}); err != nil || len(hits) != 2 {
		t.Errorf("raw OR = %+v, %v", hits, err)
	}
	if hits, _ := s.SearchMemory(ctx, "  ", MemorySearchOptions{}); len(hits) != 0 {
		t.Errorf("empty query = %+v", hits)
	}
	// The event search does not see memory.
	if hits, err := s.Search(ctx, "beacon", SearchFilter{}); err != nil || len(hits) != 0 {
		t.Errorf("event search sees memory: %+v, %v", hits, err)
	}
}

func TestMemoryStatsAndBlobUse(t *testing.T) {
	s, _ := openTestStore(t)
	if st, err := s.MemoryStats(ctx); err != nil || st != (MemoryStats{}) {
		t.Errorf("empty stats = %+v, %v", st, err)
	}
	memoryFixture(t, s)
	// The same content in another project is one blob.
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: "-w-copy", Observed: ts("2026-10-11T10:00:00Z"), Changes: []MemoryChange{
		change("notes.md", memOther, "2026-10-11T09:00:00Z"),
	}})
	st, err := s.MemoryStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Projects != 3 || st.Files != 3 || st.Deleted != 1 || st.Versions != 6 || st.Blobs != 4 ||
		st.Bytes != int64(len(memIndex)+len(memBuild)+len(memBuild2)+len(memOther)) || st.StoredBytes <= 0 {
		t.Errorf("stats = %+v", st)
	}
	uses, err := s.BlobMemory(ctx, blobref.Hash([]byte(memOther)))
	if err != nil || len(uses) != 2 || uses[0].Key != memKey2 || uses[1].Key != "-w-copy" || uses[0].Path != "notes.md" || uses[0].Harness != memHarness ||
		!uses[0].TS.Equal(ts("2026-09-15T08:00:00Z")) {
		t.Errorf("BlobMemory = %+v, %v", uses, err)
	}
	// blob reads the content like any other blob.
	if info, data, err := s.Blob(ctx, blobref.Hash([]byte(memOther))); err != nil || string(data) != memOther || info.MediaType != "text/markdown" {
		t.Errorf("Blob = %+v %q %v", info, data, err)
	}
}

func TestWriteMemoryCwd(t *testing.T) {
	s, _ := openTestStore(t)
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-01T10:00:00Z")})
	// An update without changes still records the project and a cwd found later.
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Cwd: "/w/kompass", Observed: ts("2026-10-02T10:00:00Z")})
	// An empty cwd keeps the stored one.
	writeMemory(t, s, MemoryUpdate{Harness: memHarness, Key: memKey, Observed: ts("2026-10-03T10:00:00Z")})
	state, err := s.MemoryState(ctx, memHarness)
	if err != nil || state[memKey].Cwd != "/w/kompass" || len(state[memKey].Files) != 0 {
		t.Errorf("state = %+v, %v", state, err)
	}
	if other, _ := s.MemoryState(ctx, "other"); len(other) != 0 {
		t.Errorf("other harness = %+v", other)
	}
}
