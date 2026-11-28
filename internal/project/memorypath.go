package project

import (
	"io/fs"
	"os"
	"strings"

	"github.com/seletz/total-recall/internal/store"
)

// Where the path of a memory directory comes from (MemoryPathIn).
const (
	// PathCwd: the working directory of a session of the project.
	PathCwd = "cwd"
	// PathGitRoot: the git root of a session of the project.
	PathGitRoot = "git_root"
	// PathKey: decoded from the harness key and found on the file system,
	// the only path that fits.
	PathKey = "key"
	// PathUnknown: no session, and the key does not decode to exactly one
	// existing directory. There is no path then, never a guess.
	PathUnknown = "unknown"
)

// DirReader lists a directory; os.ReadDir is the default.
type DirReader func(name string) ([]fs.DirEntry, error)

// MemoryPath is MemoryPathIn on the real file system.
func MemoryPath(p store.MemoryProject, home string) (path, source string) {
	return MemoryPathIn(p, home, os.ReadDir)
}

// MemoryPathIn is the readable place of a memory directory and where it
// comes from: the working directory or the git root of a session, else the
// path decoded from the harness key. The key is lossy (Claude Code writes
// every character but letters and digits as "-"), so a candidate counts
// only when it exists on the file system and is the only one that encodes
// to the key; otherwise the path is "" with source PathUnknown. The home
// directory is shortened to "~". The memory tab of the TUI and `memory` of
// the CLI use it, so both show the same.
func MemoryPathIn(p store.MemoryProject, home string, read DirReader) (path, source string) {
	switch {
	case p.Cwd != "":
		return shortHome(p.Cwd, home), PathCwd
	case p.GitRoot != "":
		return shortHome(p.GitRoot, home), PathGitRoot
	}
	if read != nil && strings.HasPrefix(p.Key, "-") {
		if found := decodeKey(p.Key[1:], "/", read, nil); len(found) == 1 {
			return shortHome(found[0], home), PathKey
		}
	}
	return "", PathUnknown
}

// encodeSegment is a path element as the harness writes it into a key.
func encodeSegment(s string) string {
	b := []byte(s)
	for i, c := range b {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// decodeKey finds the directories below dir whose path encodes to rest (the
// key without the leading "-"). It returns every one it finds, up to two
// (more is not needed to tell a unique path from an ambiguous one).
func decodeKey(rest, dir string, read DirReader, found []string) []string {
	entries, err := read(dir)
	if err != nil {
		return found
	}
	for _, e := range entries {
		if len(found) > 1 {
			return found
		}
		if !e.IsDir() {
			// A symlink to a directory is no DirEntry dir; follow it.
			if e.Type()&fs.ModeSymlink == 0 {
				continue
			}
		}
		enc := encodeSegment(e.Name())
		child := strings.TrimRight(dir, "/") + "/" + e.Name()
		switch {
		case rest == enc:
			found = append(found, child)
		case strings.HasPrefix(rest, enc+"-"):
			found = decodeKey(rest[len(enc)+1:], child, read, found)
		}
	}
	return found
}

func shortHome(path, home string) string {
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
// path; then the last element of a certain path reads better (the path is
// shown beside it). With an unknown path the key stays: nothing is derived
// from a guess.
func MemoryLabel(name string, p store.MemoryProject, path, source string) string {
	if source == PathUnknown {
		return p.Key
	}
	if name == p.Key || strings.ContainsAny(name, "/~") {
		return path[strings.LastIndex(path, "/")+1:]
	}
	return name
}
