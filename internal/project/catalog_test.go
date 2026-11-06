package project

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/seletz/total-recall/internal/store"
)

type fakeSource struct {
	keys []store.ProjectKey
	mem  []store.MemoryProject
}

func (f fakeSource) ProjectKeys(context.Context) ([]store.ProjectKey, error) { return f.keys, nil }
func (f fakeSource) MemoryProjects(context.Context) ([]store.MemoryProject, error) {
	return f.mem, nil
}

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	src := fakeSource{
		keys: []store.ProjectKey{
			{Key: "/home/u/src/alpha", Remote: "https://git.invalid/u/alpha"},
			{Key: "/home/u/src/beta", Remote: "https://git.invalid/u/beta"},
			{Key: "/home/u/scratch", Dir: true},
		},
		mem: []store.MemoryProject{
			{ID: 1, Harness: "claude", Key: "-home-u-src-alpha", Cwd: "/home/u/src/alpha", GitRoot: "/home/u/src/alpha", GitRemoteURL: "https://git.invalid/u/alpha"},
			{ID: 2, Harness: "claude", Key: "-home-u-old"},
		},
	}
	c, err := LoadCatalog(context.Background(), src, Resolver{}, "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCatalogEntries(t *testing.T) {
	c := testCatalog(t)
	var names []string
	for _, e := range c.Entries() {
		names = append(names, e.Name)
	}
	// The projects of the archive in its order, then a memory project of
	// an unknown directory under its native key; every name once.
	want := []string{"alpha", "beta", "~/scratch", "-home-u-old"}
	if !slices.Equal(names, want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	first := c.Entries()[0].Scope
	if !slices.Equal(first.Keys, []string{"/home/u/src/alpha"}) || !slices.Equal(first.MemoryProjectIDs, []int64{1}) {
		t.Errorf("scope of alpha = %+v: sessions and memory belong together", first)
	}
	last := c.Entries()[3].Scope
	if len(last.Keys) != 0 || !slices.Equal(last.MemoryProjectIDs, []int64{2}) {
		t.Errorf("scope of the memory-only project = %+v", last)
	}
}

func TestCatalogScopeOf(t *testing.T) {
	c := testCatalog(t)
	tests := []struct {
		name     string
		ref      store.ProjectRef
		memoryID int64
		wantName string
		keys     []string
		ids      []int64
	}{
		{"session of a project with memory", store.ProjectRef{Key: "/home/u/src/alpha", Remote: "https://git.invalid/u/alpha"}, 0, "alpha", []string{"/home/u/src/alpha"}, []int64{1}},
		{"memory hit of the same project", store.ProjectRef{Key: "/home/u/src/alpha"}, 1, "alpha", []string{"/home/u/src/alpha"}, []int64{1}},
		{"project without memory", store.ProjectRef{Key: "/home/u/src/beta"}, 0, "beta", []string{"/home/u/src/beta"}, []int64{}},
		{"memory of an unknown directory", store.ProjectRef{}, 2, "-home-u-old", []string{}, []int64{2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, sc := c.ScopeOf(tt.ref, tt.memoryID)
			if name != tt.wantName || !slices.Equal(sc.Keys, tt.keys) || !slices.Equal(sc.MemoryProjectIDs, tt.ids) {
				t.Errorf("ScopeOf = %q, %+v; want %q, keys %v, ids %v", name, sc, tt.wantName, tt.keys, tt.ids)
			}
		})
	}
}

func TestCatalogScope(t *testing.T) {
	c := testCatalog(t)
	if sc, err := c.Scope("alpha"); err != nil || len(sc.Keys) != 1 {
		t.Errorf("Scope(alpha) = %+v, %v", sc, err)
	}
	if sc, err := c.Scope("src"); err != nil || len(sc.Keys) != 2 {
		t.Errorf("Scope(src) = %+v, %v: a part of the path matches both", sc, err)
	}
	if _, err := c.Scope("nothing"); !errors.Is(err, ErrNoMatch) {
		t.Errorf("Scope(nothing) error = %v, want ErrNoMatch", err)
	}
}
