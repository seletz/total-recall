package configview

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// archive is what the projects page knows of the archive: its raw project
// keys (store.ProjectKeys) with the names they get without definitions.
type archive struct {
	keys   []store.ProjectKey
	names  map[string]string
	loaded bool
	err    error
}

// newArchive names the keys as the stats view would without definitions.
func newArchive(keys []store.ProjectKey, home string) archive {
	ks := make([]string, len(keys))
	var dirs []string
	for i, k := range keys {
		ks[i] = k.Key
		if k.Dir {
			dirs = append(dirs, k.Key)
		}
	}
	return archive{keys: keys, names: project.Names(ks, dirs, home), loaded: true}
}

// name is the derived name of the key at index i.
func (a archive) name(i int) string { return a.names[a.keys[i].Key] }

// tally counts the keys at the indexes and their sessions.
func (a archive) tally(idx []int) (paths, sessions int) {
	for _, i := range idx {
		paths++
		sessions += a.keys[i].Sessions
	}
	return paths, sessions
}

// assignment is where the keys of the archive go under a resolver: the key
// indexes per configured name and the ones no definition matches.
type assignment struct {
	byName    map[string][]int
	unmatched []int
}

// assign applies r to every key of the archive.
func assign(r project.Resolver, a archive) assignment {
	out := assignment{byName: map[string][]int{}}
	for i, k := range a.keys {
		name := r.Key(k.Key, k.Dir, k.Remote)
		if name == k.Key {
			out.unmatched = append(out.unmatched, i)
			continue
		}
		out.byName[name] = append(out.byName[name], i)
	}
	return out
}

// compile builds the definitions of the raw entries, leaving out the ones
// that do not compile; the error of the first bad entry is returned with
// them.
func compile(ps []config.Project) ([]project.Definition, error) {
	var defs []project.Definition
	var first error
	for i, p := range ps {
		d, err := project.NewDefinition(p.Name, p.Description, p.Color, p.Match)
		if err != nil {
			if first == nil {
				first = fmt.Errorf("project[%d]: %w", i, err)
			}
			continue
		}
		defs = append(defs, d)
	}
	return defs, first
}

// pageResolver is the resolver of the page: the configuration's when the
// list is fine, else the valid entries alone (so the page can still show
// what they match), else none. The error names the problem.
func pageResolver(cfg *config.Config) (project.Resolver, error) {
	if r, err := cfg.ProjectResolver(); err == nil {
		return r, nil
	}
	defs, err := compile(cfg.Projects)
	r, rerr := project.NewResolver(defs, cfg.Home)
	if rerr != nil {
		return project.Resolver{}, rerr
	}
	return r, err
}

// draftResolver is the resolver with the draft in place of the index-th
// entry (appended for -1). The draft must compile; other entries that do
// not are left out.
func draftResolver(cfg *config.Config, index int, draft config.Project) (project.Resolver, error) {
	d, err := project.NewDefinition(draft.Name, draft.Description, draft.Color, draft.Match)
	if err != nil {
		return project.Resolver{}, err
	}
	var defs []project.Definition
	for i, p := range cfg.Projects {
		if i == index {
			defs = append(defs, d)
			continue
		}
		if o, err := project.NewDefinition(p.Name, p.Description, p.Color, p.Match); err == nil {
			defs = append(defs, o)
		}
	}
	if index < 0 || index >= len(cfg.Projects) {
		defs = append(defs, d)
	}
	return project.NewResolver(defs, cfg.Home)
}

// hit is one key of the archive a pattern matches and, when another
// definition comes first, the name that takes it.
type hit struct {
	key   int
	taken string
}

// hits lists the keys that the patterns match and who gets each of them
// under r, in the order of the archive.
func hits(r project.Resolver, name string, patterns []string, a archive, home string) ([]hit, error) {
	ps, err := project.CompilePatterns(patterns)
	if err != nil {
		return nil, err
	}
	var out []hit
	for i, k := range a.keys {
		if !ps.Matches(project.Targets(k.Key, k.Dir, k.Remote, home)...) {
			continue
		}
		h := hit{key: i}
		if got := r.Key(k.Key, k.Dir, k.Remote); got != name {
			h.taken = got
		}
		out = append(out, h)
	}
	return out, nil
}

// describe formats the hits of a definition or pattern: the paths and
// sessions it gets with their names, and the ones an earlier definition
// takes.
func (a archive) describe(hs []hit) string {
	if len(hs) == 0 {
		return "matches nothing in the archive"
	}
	var mine, taken []int
	winner := map[int]string{}
	for _, h := range hs {
		if h.taken == "" {
			mine = append(mine, h.key)
		} else {
			taken = append(taken, h.key)
			winner[h.key] = h.taken
		}
	}
	var parts []string
	if len(mine) > 0 {
		paths, sessions := a.tally(mine)
		names := make([]string, len(mine))
		for i, k := range mine {
			names[i] = a.name(k)
		}
		parts = append(parts, fmt.Sprintf("matches %s · %s: %s", plural(paths, "path"), plural(sessions, "session"), strings.Join(names, ", ")))
	}
	for _, k := range taken {
		parts = append(parts, fmt.Sprintf("%s goes to %s (earlier)", a.name(k), winner[k]))
	}
	return strings.Join(parts, " · ")
}

// suggest is the definition a key of the archive would get: the derived
// name (the base name for a directory) and the remote URL as the pattern,
// else the path with "~"; the remote URL covers every worktree and
// checkout of the repository.
func suggest(k store.ProjectKey, a archive, home string) config.Project {
	name := a.names[k.Key]
	if k.Dir || name == "" {
		name = filepath.Base(k.Key)
	}
	return config.Project{Name: name, Match: config.StringList{suggestPattern(k, home)}}
}

// suggestPattern is the pattern that names a key best: the remote URL of a
// repository, else the path with "~".
func suggestPattern(k store.ProjectKey, home string) string {
	if k.Remote != "" {
		return k.Remote
	}
	return project.Tilde(k.Key, home)
}
