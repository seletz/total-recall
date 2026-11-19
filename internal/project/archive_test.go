package project

import (
	"reflect"
	"testing"

	"github.com/seletz/total-recall/internal/store"
)

func testArchive(t *testing.T) (Archive, Resolver) {
	t.Helper()
	keys := []store.ProjectKey{
		{Key: "/home/mara/src/kompass", Remote: "https://git.invalid/mara/kompass", Sessions: 5},
		{Key: "/home/mara/wt/kompass-fix", Remote: "https://git.invalid/mara/kompass", Sessions: 2},
		{Key: "/home/mara/src/wetterwarte", Sessions: 3},
		{Key: "/home/mara/scratch", Dir: true, Sessions: 1},
	}
	a := NewArchive(keys, "/home/mara")
	k, err := NewDefinition("kompass", "", "", []string{"https://git.invalid/mara/kompass"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewDefinition("wetter", "", "", []string{"~/src/wetter*"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewResolver([]Definition{k, w}, "/home/mara")
	if err != nil {
		t.Fatal(err)
	}
	return a, r
}

func TestArchiveAssign(t *testing.T) {
	a, r := testArchive(t)
	as := a.Assign(r)
	if !reflect.DeepEqual(as.ByName["kompass"], []int{0, 1}) || !reflect.DeepEqual(as.ByName["wetter"], []int{2}) || !reflect.DeepEqual(as.Unmatched, []int{3}) {
		t.Errorf("assignment = %+v", as)
	}
	// Sum = parts: every key is in exactly one place.
	paths, sessions := 0, 0
	for _, idx := range as.ByName {
		p, s := a.Tally(idx)
		paths, sessions = paths+p, sessions+s
	}
	p, s := a.Tally(as.Unmatched)
	if paths+p != len(a.Keys) || sessions+s != 11 {
		t.Errorf("paths %d+%d, sessions %d+%d", paths, p, sessions, s)
	}
	if got := a.Name(0); got != "kompass" {
		t.Errorf("Name(0) = %q", got)
	}
}

func TestArchiveHitsAndDescribe(t *testing.T) {
	a, r := testArchive(t)
	tests := []struct {
		name     string
		patterns []string
		def      string
		want     []Hit
		desc     string
	}{
		{"remote covers worktrees", []string{"https://git.invalid/mara/kompass"}, "kompass", []Hit{{Key: 0}, {Key: 1}}, "matches 2 paths · 7 sessions: kompass, kompass-fix"},
		{"nothing", []string{"~/nowhere"}, "kompass", nil, "matches nothing in the archive"},
		{"taken by an earlier definition", []string{"~/src/*"}, "wetter", []Hit{{Key: 0, Taken: "kompass"}, {Key: 2}}, "matches 1 path · 3 sessions: wetterwarte · kompass goes to kompass (earlier)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.Hits(r, tt.def, tt.patterns, "/home/mara")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("hits = %+v, want %+v", got, tt.want)
			}
			if d := a.Describe(got); d != tt.desc {
				t.Errorf("describe = %q, want %q", d, tt.desc)
			}
		})
	}
	if _, err := a.Hits(r, "x", []string{""}, "/home/mara"); err == nil {
		t.Error("an empty pattern is accepted")
	}
}
