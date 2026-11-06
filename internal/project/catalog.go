package project

import (
	"context"
	"fmt"
	"strings"

	"github.com/seletz/total-recall/internal/store"
)

// ErrNoMatch is wrapped by Catalog.Scope when no project matches.
var ErrNoMatch = fmt.Errorf("no project matches")

// Source is what a catalog is built from; *store.Store implements it.
type Source interface {
	ProjectKeys(ctx context.Context) ([]store.ProjectKey, error)
	MemoryProjects(ctx context.Context) ([]store.MemoryProject, error)
}

// Catalog names raw project keys like stats --by project (the
// configured definition, else the derived name) over every key of the
// archive and of the memory, and resolves --project.
type Catalog struct {
	res   Resolver
	names map[string]string // mapped key -> display name
	keys  []catalogKey
	mem   []catalogMemory
}

type catalogKey struct {
	ref  store.ProjectRef
	name string
}

type catalogMemory struct {
	p    store.MemoryProject
	ref  store.ProjectRef
	name string
}

func memoryRef(p store.MemoryProject) store.ProjectRef {
	switch {
	case p.GitRoot != "":
		return store.ProjectRef{Key: p.GitRoot, Remote: p.GitRemoteURL}
	case p.Cwd != "":
		return store.ProjectRef{Key: p.Cwd, Dir: true}
	}
	return store.ProjectRef{}
}

// LoadCatalog reads every project key of the archive and every memory
// project from st and names them with res; home shortens paths in the
// derived names.
func LoadCatalog(ctx context.Context, st Source, res Resolver, home string) (*Catalog, error) {
	pks, err := st.ProjectKeys(ctx)
	if err != nil {
		return nil, err
	}
	mps, err := st.MemoryProjects(ctx)
	if err != nil {
		return nil, err
	}
	c := &Catalog{res: res}
	var refs []store.ProjectRef
	for _, k := range pks {
		r := store.ProjectRef{Key: k.Key, Dir: k.Dir, Remote: k.Remote}
		if r.Dir {
			r.Remote = ""
		}
		refs = append(refs, r)
		c.keys = append(c.keys, catalogKey{ref: r})
	}
	for _, p := range mps {
		r := memoryRef(p)
		c.mem = append(c.mem, catalogMemory{p: p, ref: r})
		if r.Key != "" {
			refs = append(refs, r)
		}
	}
	var mapped, dirs []string
	for _, r := range refs {
		m := res.Key(r.Key, r.Dir, r.Remote)
		mapped = append(mapped, m)
		if r.Dir && m == r.Key {
			dirs = append(dirs, r.Key)
		}
	}
	c.names = res.Names(mapped, dirs, home)
	for i := range c.keys {
		c.keys[i].name = c.Name(c.keys[i].ref)
	}
	for i := range c.mem {
		c.mem[i].name = c.mem[i].p.Key
		if c.mem[i].ref.Key != "" {
			c.mem[i].name = c.Name(c.mem[i].ref)
		}
	}
	return c, nil
}

// name is the display name of a raw project key, "" for none.
func (c *Catalog) Name(r store.ProjectRef) string {
	if r.Key == "" {
		return ""
	}
	m := c.res.Key(r.Key, r.Dir, r.Remote)
	if n, ok := c.names[m]; ok {
		return n
	}
	return m
}

// memoryName is the display name of a memory hit's project: its project
// name, else the harness's native key.
func (c *Catalog) MemoryName(h *store.MemoryHit) string {
	if n := c.Name(h.Project); n != "" {
		return n
	}
	return h.ProjectKey
}

// scope resolves --project: the raw keys and memory projects whose name,
// key, path, remote URL or native memory key equals ref, else those that
// contain it. A memory project and the sessions with
// the same raw key go together.
func (c *Catalog) Scope(ref string) (*store.ProjectScope, error) {
	pick := func(match func(string) bool) *store.ProjectScope {
		sc := &store.ProjectScope{Keys: []string{}, MemoryProjectIDs: []int64{}}
		keys := map[string]bool{}
		for _, k := range c.keys {
			if match(k.name) || match(k.ref.Key) || match(k.ref.Remote) {
				keys[k.ref.Key] = true
			}
		}
		for _, m := range c.mem {
			if match(m.name) || match(m.p.Key) || match(m.p.Cwd) || match(m.p.GitRoot) || match(m.p.GitRemoteURL) {
				sc.MemoryProjectIDs = append(sc.MemoryProjectIDs, m.p.ID)
				if m.ref.Key != "" {
					keys[m.ref.Key] = true
				}
			}
		}
		for _, k := range c.keys {
			if keys[k.ref.Key] {
				sc.Keys = append(sc.Keys, k.ref.Key)
			}
		}
		for _, m := range c.mem {
			if m.ref.Key != "" && keys[m.ref.Key] && !containsID(sc.MemoryProjectIDs, m.p.ID) {
				sc.MemoryProjectIDs = append(sc.MemoryProjectIDs, m.p.ID)
			}
		}
		if len(keys) == 0 && len(sc.MemoryProjectIDs) == 0 {
			return nil
		}
		return sc
	}
	if sc := pick(func(s string) bool { return s != "" && s == ref }); sc != nil {
		return sc, nil
	}
	if sc := pick(func(s string) bool { return s != "" && strings.Contains(s, ref) }); sc != nil {
		return sc, nil
	}
	return nil, fmt.Errorf("%w %q", ErrNoMatch, ref)
}

func containsID(ids []int64, id int64) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}

// Entry is one project of the catalog as the pin of the TUI search lists
// it: the display name and what it restricts a typed search to.
type Entry struct {
	Name  string
	Scope *store.ProjectScope
}

// Entries lists the projects by display name, the one with the most
// sessions first, memory-only projects after. Every name appears once; its
// scope is the one --project NAME resolves to.
func (c *Catalog) Entries() []Entry {
	var out []Entry
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		if sc, err := c.Scope(name); err == nil {
			out = append(out, Entry{Name: name, Scope: sc})
		}
	}
	for _, k := range c.keys {
		add(k.name)
	}
	for _, m := range c.mem {
		add(m.name)
	}
	return out
}

// ScopeOf is the project of one hit: its display name and the scope of
// everything with the same raw key. memoryID is the ID of the hit's
// memory project, 0 for other hits; it is part of the scope even when
// its directory is unknown.
func (c *Catalog) ScopeOf(ref store.ProjectRef, memoryID int64) (string, *store.ProjectScope) {
	sc := &store.ProjectScope{Keys: []string{}, MemoryProjectIDs: []int64{}}
	name := c.Name(ref)
	if ref.Key != "" {
		sc.Keys = append(sc.Keys, ref.Key)
	}
	for _, m := range c.mem {
		if m.p.ID == memoryID || ref.Key != "" && m.ref.Key == ref.Key {
			sc.MemoryProjectIDs = append(sc.MemoryProjectIDs, m.p.ID)
			if name == "" {
				name = m.name
			}
		}
	}
	if name == "" {
		name = ref.Key
	}
	return name, sc
}
