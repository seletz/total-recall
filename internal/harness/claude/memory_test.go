package claude

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/harness"
)

var _ harness.MemorySource = (*Adapter)(nil)

func writeMemFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// memoryTree builds a synthetic projects dir: kompass with a transcript
// and memory (a sub directory, files that are skipped), lighthouse with
// memory but no transcript, a project without memory and one whose memory
// directory is empty.
func memoryTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	k := filepath.Join(dir, "-w-kompass")
	writeMemFile(t, filepath.Join(k, "s1.jsonl"), `{"type":"summary","summary":"x"}`+"\n"+`{"type":"user","cwd":"/w/kompass","sessionId":"s1"}`+"\n")
	writeMemFile(t, filepath.Join(k, "memory", "MEMORY.md"), "# Memory\n")
	writeMemFile(t, filepath.Join(k, "memory", "build.md"), "build notes\n")
	writeMemFile(t, filepath.Join(k, "memory", "topics", "deep.md"), "deep\n")
	writeMemFile(t, filepath.Join(k, "memory", ".hidden.md"), "editor swap\n")
	writeMemFile(t, filepath.Join(k, "memory", "picture.png"), "\x89PNG")
	if err := os.Symlink(filepath.Join(k, "memory", "build.md"), filepath.Join(k, "memory", "link.md")); err != nil {
		t.Fatal(err)
	}
	writeMemFile(t, filepath.Join(dir, "-w-lighthouse", "memory", "notes.md"), "beacon\n")
	writeMemFile(t, filepath.Join(dir, "-w-nomemory", "s2.jsonl"), `{"type":"user","cwd":"/w/nomemory"}`+"\n")
	if err := os.MkdirAll(filepath.Join(dir, "-w-emptied", "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMemory(t *testing.T) {
	dir := memoryTree(t)
	mtime := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "-w-kompass", "memory", "build.md"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	projects, ok, err := New(dir).Memory()
	if err != nil || !ok {
		t.Fatalf("Memory = %v, %v", ok, err)
	}
	keys := []string{}
	for _, p := range projects {
		keys = append(keys, p.Key)
	}
	if !slices.Equal(keys, []string{"-w-emptied", "-w-kompass", "-w-lighthouse"}) {
		t.Fatalf("keys = %q", keys)
	}
	if len(projects[0].Files) != 0 {
		t.Errorf("emptied = %+v", projects[0].Files)
	}
	k := projects[1]
	paths := []string{}
	for _, f := range k.Files {
		paths = append(paths, f.Path)
	}
	if !slices.Equal(paths, []string{"MEMORY.md", "build.md", "topics/deep.md"}) {
		t.Errorf("kompass files = %q", paths)
	}
	b := k.Files[1]
	if b.Abs != filepath.Join(dir, "-w-kompass", "memory", "build.md") || b.Size != int64(len("build notes\n")) || !b.MTime.Equal(mtime) {
		t.Errorf("build.md = %+v", b)
	}
}

func TestMemoryMissingDir(t *testing.T) {
	projects, ok, err := New(filepath.Join(t.TempDir(), "nope")).Memory()
	if err != nil || ok || len(projects) != 0 {
		t.Errorf("missing dir = %+v, %v, %v; want nothing, not ok", projects, ok, err)
	}
}

func TestMemoryCwd(t *testing.T) {
	dir := memoryTree(t)
	a := New(dir)
	for _, c := range []struct {
		key        string
		candidates []string
		want       string
	}{
		{"-w-kompass", nil, "/w/kompass"},                                     // from the transcript
		{"-w-kompass", []string{"/w/other"}, "/w/kompass"},                    // the transcript wins
		{"-w-lighthouse", []string{"/x/y", "/w/lighthouse"}, "/w/lighthouse"}, // from an archived session
		{"-w-lighthouse", []string{"/x/y"}, ""},
		{"-w-gone", nil, ""},
	} {
		if got := a.MemoryCwd(c.key, c.candidates); got != c.want {
			t.Errorf("MemoryCwd(%q, %q) = %q, want %q", c.key, c.candidates, got, c.want)
		}
	}
}

func TestProjectKey(t *testing.T) {
	for in, want := range map[string]string{
		"/w/kompass":                       "-w-kompass",
		"/Users/mara/.dotfiles":            "-Users-mara--dotfiles",
		"/src/total-recall__worktrees/a.b": "-src-total-recall--worktrees-a-b",
		"/home/mara/Projekte/Übung 1":      "-home-mara-Projekte--bung-1",
	} {
		if got := ProjectKey(in); got != want {
			t.Errorf("ProjectKey(%q) = %q, want %q", in, got, want)
		}
	}
}
