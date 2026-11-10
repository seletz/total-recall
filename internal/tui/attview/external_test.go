package attview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemViewerWritesAndStarts(t *testing.T) {
	var started string
	v := &SystemViewer{Command: func(path string) *exec.Cmd {
		started = path
		return exec.Command("true")
	}}
	path, err := v.Open("abc123.png", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(path)) })
	if started != path {
		t.Errorf("started %q, want %q", started, path)
	}
	if got, _ := os.ReadFile(path); string(got) != "data" {
		t.Errorf("file = %q", got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Dir(path)); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", fi.Mode().Perm())
	}
	// A second file lands in the same directory.
	p2, _ := v.Open("def.pdf", []byte("x"))
	if filepath.Dir(p2) != filepath.Dir(path) {
		t.Errorf("directories differ: %q %q", p2, path)
	}
}

func TestSystemViewerName(t *testing.T) {
	v := &SystemViewer{Command: func(string) *exec.Cmd { return exec.Command("true") }}
	path, err := v.Open("../../evil.png", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(path)) })
	if filepath.Base(path) != "evil.png" || !strings.HasPrefix(path, os.TempDir()) && !strings.Contains(path, "total-recall-") {
		t.Errorf("path = %q, want a file directly in the private directory", path)
	}
}

func TestSystemViewerStartFails(t *testing.T) {
	v := &SystemViewer{Command: func(string) *exec.Cmd { return exec.Command("/nonexistent/program") }}
	path, err := v.Open("a.txt", []byte("x"))
	if path != "" {
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(path)) })
	}
	if err == nil || path == "" {
		t.Errorf("Open = %q, %v; want the path and an error", path, err)
	}
}
