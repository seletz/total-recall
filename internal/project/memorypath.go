package project

import (
	"strings"

	"github.com/seletz/total-recall/internal/store"
)

// MemoryPath is the readable place of a memory directory: the working
// directory, else the git root, else the path decoded from the harness
// key ("/" was replaced by "-"). The home directory is shortened to "~".
// The memory tab of the TUI and `memory` of the CLI use it, so both tell
// the directories of one project apart the same way.
func MemoryPath(p store.MemoryProject, home string) string {
	path := p.Cwd
	if path == "" {
		path = p.GitRoot
	}
	if path == "" {
		path = strings.ReplaceAll(p.Key, "-", "/")
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}
	if home = strings.TrimRight(home, "/"); home != "" {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, home+"/"); ok {
			return "~/" + rest
		}
	}
	return path
}

// MemoryLabel is the name shown for a memory project: name as the project
// resolver gives it, except when that is the raw key of the harness or a
// path; then the last element of path reads better (the path is shown
// beside it).
func MemoryLabel(name string, p store.MemoryProject, path string) string {
	if name == p.Key || strings.ContainsAny(name, "/~") {
		return path[strings.LastIndex(path, "/")+1:]
	}
	return name
}
