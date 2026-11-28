package project

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/seletz/total-recall/internal/store"
)

// dirReader reads directories from an in-memory tree ("/home/mara/…").
func dirReader(paths ...string) DirReader {
	m := fstest.MapFS{}
	for _, p := range paths {
		m[p[1:]+"/.keep"] = &fstest.MapFile{}
	}
	return func(name string) ([]fs.DirEntry, error) {
		return fs.ReadDir(m, map[bool]string{true: ".", false: name[1:]}[name == "/"])
	}
}

// TestMemoryPath: a known working directory or git root is certain; a key
// is decoded against the file system (a "-" may be a "/", a "-", a "_" or a
// ".") and is certain only when exactly one path fits; otherwise the path
// is unknown and never a guess (#382).
func TestMemoryPath(t *testing.T) {
	tree := dirReader("/home/mara/src/mein-projekt/web-app", "/home/mara/src/kompass__worktrees/gpx", "/home/mara/src/a.b", "/home/mara/src/a-b")
	for _, tt := range []struct {
		name     string
		p        store.MemoryProject
		home     string
		read     DirReader
		wantPath string
		wantSrc  string
	}{
		{"cwd", store.MemoryProject{Cwd: "/home/mara/src/x", Key: "-k"}, "/home/mara", tree, "~/src/x", PathCwd},
		{"git root", store.MemoryProject{GitRoot: "/home/mara/src/x", Key: "-k"}, "/home/mara", tree, "~/src/x", PathGitRoot},
		{"hyphens in the directory names", store.MemoryProject{Key: "-home-mara-src-mein-projekt-web-app"}, "/home/mara", tree, "~/src/mein-projekt/web-app", PathKey},
		{"underscores", store.MemoryProject{Key: "-home-mara-src-kompass--worktrees-gpx"}, "/home/mara", tree, "~/src/kompass__worktrees/gpx", PathKey},
		{"two paths fit", store.MemoryProject{Key: "-home-mara-src-a-b"}, "/home/mara", tree, "", PathUnknown},
		{"no such directory", store.MemoryProject{Key: "-home-mara-src-gone-away"}, "/home/mara", tree, "", PathUnknown},
		{"other machine", store.MemoryProject{Key: "-Users-x-src-y"}, "/home/mara", tree, "", PathUnknown},
		{"no reader", store.MemoryProject{Key: "-home-mara-src-kompass"}, "/home/mara", nil, "", PathUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path, src := MemoryPathIn(tt.p, tt.home, tt.read)
			if path != tt.wantPath || src != tt.wantSrc {
				t.Errorf("MemoryPathIn = %q, %q; want %q, %q", path, src, tt.wantPath, tt.wantSrc)
			}
		})
	}
}

// TestMemoryLabel: the name comes from a certain path; with an unknown one
// the key stays.
func TestMemoryLabel(t *testing.T) {
	p := store.MemoryProject{Key: "-home-mara-src-gone-away"}
	if got := MemoryLabel(p.Key, p, "", PathUnknown); got != p.Key {
		t.Errorf("unknown path: label = %q, want the key", got)
	}
	if got := MemoryLabel(p.Key, p, "~/src/mein-projekt", PathKey); got != "mein-projekt" {
		t.Errorf("label = %q", got)
	}
	if got := MemoryLabel("kompass", p, "~/src/kompass", PathCwd); got != "kompass" {
		t.Errorf("label = %q", got)
	}
}
