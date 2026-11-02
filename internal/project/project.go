// Package project derives display names for projects from the raw keys of
// the activity dimension project: a session's git root, else its working
// directory. The functions are pure; they never touch the file system.
//
// The heuristic is the one of docs/spike-tui.md: the base name of the git
// root; without a repository the base name of the directory marked with "~";
// and when two keys share a base name, as many parent directories as it
// takes to tell them apart.
package project

import (
	"path/filepath"
	"strings"
)

// Name returns the project name of a session. A session inside a repository
// is named after the base name of gitRoot, so sessions in a worktree (which
// the store already resolves to the main repository) share the name of the
// repository. Without a repository the base name of cwd is used, marked with
// a leading "~". Both empty gives "".
func Name(gitRoot, cwd string) string {
	switch {
	case gitRoot != "":
		return filepath.Base(filepath.Clean(gitRoot))
	case cwd != "":
		return "~" + filepath.Base(filepath.Clean(cwd))
	}
	return ""
}

// Names maps every key of a series to a unique project name. A key is the
// path of a git root or, for sessions outside a repository, of a working
// directory; since the key alone does not tell which, no "~" marker is
// added. Names that collide, like /w/iris and /w/iris/iris, grow by one
// parent directory at a time (iris/iris, w/iris) until they are unique; a
// key that runs out of parents keeps its full path. Keys that are not paths,
// such as the store's "other", map to themselves. Duplicate keys are mapped
// once, an empty key maps to "".
func Names(keys []string) map[string]string {
	type entry struct {
		parts []string // path components, root first
		depth int      // components used for the name, counted from the end
		full  bool     // all components used and still ambiguous: use the key
	}
	entries := map[string]*entry{}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if _, dup := entries[k]; dup {
			continue
		}
		if k == "" {
			out[k] = ""
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
