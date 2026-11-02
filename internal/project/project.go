// Package project derives display names for projects from the raw keys of
// the activity dimension project: a session's git root, else its working
// directory. The functions are pure; they never touch the file system.
//
// The heuristic follows docs/spike-tui.md: the base name of the git root,
// and when two roots share a base name, as many parent directories as it
// takes to tell them apart. A working directory without a repository is
// shown as a path, with the home directory abbreviated to "~".
package project

import (
	"path/filepath"
	"strings"
)

// Names maps every key of a series to a unique project name. A key is the
// path of a git root or, for sessions outside a repository, of a working
// directory; dirs lists the keys of the latter kind (store.ActivitySeries
// reports them). Repository names that collide, like /w/iris and
// /w/iris/iris, grow by one parent directory at a time (iris/iris, w/iris)
// until they are unique; a key that runs out of parents keeps its full
// path. Directories in dirs are named by their path, abbreviated with
// Tilde(path, home); they never collide with repository names because the
// path form keeps its leading "/" or "~". Keys that are not paths, such as
// the store's "other", map to themselves. Duplicate keys are mapped once, an
// empty key maps to "".
func Names(keys []string, dirs []string, home string) map[string]string {
	type entry struct {
		parts []string // path components, root first
		depth int      // components used for the name, counted from the end
		full  bool     // all components used and still ambiguous: use the key
	}
	isDir := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		isDir[d] = true
	}
	entries := map[string]*entry{}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if _, dup := entries[k]; dup {
			continue
		}
		if _, dup := out[k]; dup {
			continue
		}
		switch {
		case k == "":
			out[k] = ""
			continue
		case isDir[k]:
			out[k] = Tilde(k, home)
			continue
		}
		clean := filepath.Clean(k)
		parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
		entries[k] = &entry{parts: parts, depth: 1}
	}
	name := func(k string, e *entry) string {
		if e.full {
			return filepath.Clean(k)
		}
		return strings.Join(e.parts[len(e.parts)-e.depth:], "/")
	}
	for {
		groups := map[string][]string{}
		for k, e := range entries {
			n := name(k, e)
			groups[n] = append(groups[n], k)
		}
		changed := false
		for _, ks := range groups {
			if len(ks) < 2 {
				continue
			}
			for _, k := range ks {
				e := entries[k]
				switch {
				case e.full:
				case e.depth < len(e.parts):
					e.depth++
					changed = true
				default:
					e.full = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	for k, e := range entries {
		out[k] = name(k, e)
	}
	return out
}

// Tilde returns path with the home directory replaced by "~": home itself
// becomes "~", a path below it "~/rest". Trailing slashes on either side do
// not matter. Paths outside home, relative paths and an empty path are
// returned cleaned but otherwise unchanged; an empty home or the root
// directory as home never abbreviates.
func Tilde(path, home string) string {
	if path == "" {
		return ""
	}
	p := filepath.Clean(path)
	if home == "" {
		return p
	}
	h := filepath.Clean(home)
	if h == "/" {
		return p
	}
	switch {
	case p == h:
		return "~"
	case strings.HasPrefix(p, h+"/"):
		return "~" + p[len(h):]
	}
	return p
}
