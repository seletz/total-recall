package project

import (
	"fmt"
	"strings"

	"github.com/seletz/total-recall/internal/store"
)

// Archive is what the project definitions are tried against: the raw
// project keys of the archive (store.ProjectKeys) with the names they get
// without definitions. The projects page of the TUI and `config projects`
// share it.
type Archive struct {
	// Keys are the raw project keys, in the order store.ProjectKeys gave.
	Keys  []store.ProjectKey
	names map[string]string
}

// NewArchive names the keys as the stats view would without definitions;
// home is the home directory "~" stands for.
func NewArchive(keys []store.ProjectKey, home string) Archive {
	ks := make([]string, len(keys))
	var dirs []string
	for i, k := range keys {
		ks[i] = k.Key
		if k.Dir {
			dirs = append(dirs, k.Key)
		}
	}
	return Archive{Keys: keys, names: Names(ks, dirs, home)}
}

// Name is the derived name of the key at index i.
func (a Archive) Name(i int) string { return a.names[a.Keys[i].Key] }

// NameOf is the derived name of a raw key.
func (a Archive) NameOf(key string) string { return a.names[key] }

// Tally counts the keys at the indexes and their sessions.
func (a Archive) Tally(idx []int) (paths, sessions int) {
	for _, i := range idx {
		paths++
		sessions += a.Keys[i].Sessions
	}
	return paths, sessions
}

// Assignment is where the keys of the archive go under a resolver: the key
// indexes per configured name and the ones no definition matches.
type Assignment struct {
	ByName    map[string][]int
	Unmatched []int
}

// Assign applies r to every key of the archive. Every key ends up in
// exactly one place.
func (a Archive) Assign(r Resolver) Assignment {
	out := Assignment{ByName: map[string][]int{}}
	for i, k := range a.Keys {
		name := r.Key(k.Key, k.Dir, k.Remote)
		if name == k.Key {
			out.Unmatched = append(out.Unmatched, i)
			continue
		}
		out.ByName[name] = append(out.ByName[name], i)
	}
	return out
}

// Hit is one key of the archive a pattern matches and, when another
// definition comes first, the name that takes it.
type Hit struct {
	Key   int
	Taken string
}

// Hits lists the keys that the patterns match and who gets each of them
// under r (a resolver that has the definition called name), in the order
// of the archive.
func (a Archive) Hits(r Resolver, name string, patterns []string, home string) ([]Hit, error) {
	ps, err := CompilePatterns(patterns)
	if err != nil {
		return nil, err
	}
	var out []Hit
	for i, k := range a.Keys {
		if !ps.Matches(Targets(k.Key, k.Dir, k.Remote, home)...) {
			continue
		}
		h := Hit{Key: i}
		if got := r.Key(k.Key, k.Dir, k.Remote); got != name {
			h.Taken = got
		}
		out = append(out, h)
	}
	return out, nil
}

// Describe formats the hits of a definition or pattern: the paths and
// sessions it gets with their names, and the ones an earlier definition
// takes.
func (a Archive) Describe(hs []Hit) string {
	if len(hs) == 0 {
		return "matches nothing in the archive"
	}
	var mine, taken []int
	winner := map[int]string{}
	for _, h := range hs {
		if h.Taken == "" {
			mine = append(mine, h.Key)
		} else {
			taken = append(taken, h.Key)
			winner[h.Key] = h.Taken
		}
	}
	var parts []string
	if len(mine) > 0 {
		paths, sessions := a.Tally(mine)
		names := make([]string, len(mine))
		for i, k := range mine {
			names[i] = a.Name(k)
		}
		parts = append(parts, fmt.Sprintf("matches %s · %s: %s", plural(paths, "path"), plural(sessions, "session"), strings.Join(names, ", ")))
	}
	for _, k := range taken {
		parts = append(parts, fmt.Sprintf("%s goes to %s (earlier)", a.Name(k), winner[k]))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
