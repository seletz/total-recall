package project

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCompilePatterns(t *testing.T) {
	tests := []struct {
		pattern string
		target  string
		want    bool
	}{
		{"kompass", "kompass", true},
		{"kompass", "kompass-gpx", false},
		{"kompass*", "kompass-gpx", true},
		{"https://git.invalid/mara/*", "https://git.invalid/mara/kompass", true},
		{"*git.invalid/mara/*", "https://git.invalid/mara/kompass", true}, // * crosses "/"
		{"*/mara/*", "https://git.invalid/other/kompass", false},
		{"~/src/*", "~/src/kompass", true},
		{"~/src/*", "/home/mara/src/kompass", false},
		{"k?mpass", "kompass", true},
		{"k?mpass", "koompass", false},
		{"*", "", true},
		{"a.b", "axb", false}, // a dot is literal
		{"[x]", "[x]", true},  // brackets are literal
	}
	for _, tt := range tests {
		p, err := CompilePatterns([]string{tt.pattern})
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Matches(tt.target); got != tt.want {
			t.Errorf("%q matches %q = %v, want %v", tt.pattern, tt.target, got, tt.want)
		}
	}
	p, err := CompilePatterns([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Matches("x", "b") || p.Matches("x", "y") {
		t.Error("any pattern against any target")
	}
	if _, err := CompilePatterns(nil); err == nil {
		t.Error("no patterns: no error")
	}
	if _, err := CompilePatterns([]string{"a", ""}); err == nil {
		t.Error("empty pattern: no error")
	}
	if (Patterns{}).Matches("anything") {
		t.Error("zero Patterns matches")
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"", "", true},
		{"blue", "#4e79a7", true},
		{"Teal", "#76b7b2", true},
		{" orange ", "#f28e2b", true},
		{"#1A2B3C", "#1a2b3c", true},
		{"#abc", "", false},
		{"1a2b3c", "", false},
		{"#1a2b3g", "", false},
		{"mauve", "", false},
	}
	for _, tt := range tests {
		got, err := ParseColor(tt.in)
		if (err == nil) != tt.ok {
			t.Errorf("ParseColor(%q) error = %v, want ok %v", tt.in, err, tt.ok)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseColor(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	for _, c := range Palette {
		if got, err := ParseColor(c.Name); err != nil || got != c.Hex {
			t.Errorf("ParseColor(%q) = %q, %v, want %q", c.Name, got, err, c.Hex)
		}
	}
}

func TestNewDefinition(t *testing.T) {
	tests := []struct {
		name           string
		defName, color string
		match          []string
		wantErr        string
		wantColor      string
	}{
		{"minimal", "kompass", "", []string{"kompass"}, "", ""},
		{"named colour", "kompass", "green", []string{"kompass"}, "", "#59a14f"},
		{"hex colour", "kompass", "#ABCDEF", []string{"kompass"}, "", "#abcdef"},
		{"no name", "", "", []string{"x"}, "without a name", ""},
		{"blank name", "  ", "", []string{"x"}, "without a name", ""},
		{"no patterns", "kompass", "", nil, "no match patterns", ""},
		{"empty pattern", "kompass", "", []string{""}, "empty match pattern", ""},
		{"bad colour", "kompass", "pink", []string{"x"}, `color "pink"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewDefinition(tt.defName, "desc", tt.color, tt.match)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.Name != tt.defName || d.Description != "desc" || d.Color != tt.wantColor || !reflect.DeepEqual(d.Match, tt.match) {
				t.Errorf("definition = %+v", d)
			}
		})
	}
}

func mustDef(t *testing.T, name, color string, match ...string) Definition {
	t.Helper()
	d, err := NewDefinition(name, "", color, match)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestNewResolverDuplicate(t *testing.T) {
	a := mustDef(t, "kompass", "", "a")
	b := mustDef(t, "iris", "", "b")
	if _, err := NewResolver([]Definition{a, b}, ""); err != nil {
		t.Fatal(err)
	}
	a2 := mustDef(t, "kompass", "", "c")
	if _, err := NewResolver([]Definition{a, b, a2}, ""); err == nil || !strings.Contains(err.Error(), `"kompass"`) {
		t.Errorf("duplicate name: error = %v", err)
	}
}

func TestResolverKey(t *testing.T) {
	defs := []Definition{
		mustDef(t, "kompass", "blue", "https://git.invalid/mara/kompass*"),
		mustDef(t, "wetterwarte", "", "~/src/wetter*", "/srv/wetterwarte"),
		mustDef(t, "notizbox", "", "~/notes"),
		mustDef(t, "iris", "", "iris"),
		mustDef(t, "rest", "", "*"),
	}
	r, err := NewResolver(defs, "/home/mara")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		key    string
		dir    bool
		remote string
		want   string
	}{
		{"remote url", "/w/kompass-api", false, "https://git.invalid/mara/kompass-api", "kompass"},
		{"second worktree by remote", "/w/kompass__worktrees/x", false, "https://git.invalid/mara/kompass", "kompass"},
		{"path with tilde", "/home/mara/src/wetterwarte", false, "", "wetterwarte"},
		{"absolute path", "/srv/wetterwarte", false, "https://git.invalid/other/ww", "wetterwarte"},
		{"directory with tilde", "/home/mara/notes", true, "", "notizbox"},
		{"derived name", "/x/y/iris", false, "https://git.invalid/mara/iris-app", "iris"},
		{"first definition wins", "/home/mara/src/wetter-iris", false, "https://git.invalid/mara/kompass-x", "kompass"},
		{"catch-all", "/elsewhere/thing", false, "", "rest"},
		{"other is never mapped", "other", false, "", "other"},
		{"empty key", "", false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Key(tt.key, tt.dir, tt.remote); got != tt.want {
				t.Errorf("Key(%q, %v, %q) = %q, want %q", tt.key, tt.dir, tt.remote, got, tt.want)
			}
		})
	}
	var zero Resolver
	if got := zero.Key("/w/kompass", false, "https://git.invalid/mara/kompass"); got != "/w/kompass" {
		t.Errorf("zero resolver maps %q", got)
	}
}

func TestResolverNoCatchAll(t *testing.T) {
	r, err := NewResolver([]Definition{mustDef(t, "kompass", "", "https://git.invalid/mara/kompass")}, "/home/mara")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Key("/w/iris", false, "https://git.invalid/mara/iris"); got != "/w/iris" {
		t.Errorf("unmatched key = %q, want the key", got)
	}
}

func TestResolverNames(t *testing.T) {
	r, err := NewResolver([]Definition{
		mustDef(t, "kompass", "blue", "*kompass*"),
		mustDef(t, "iris", "", "https://git.invalid/mara/iris"),
	}, "/home/mara")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		keys []string
		dirs []string
		want map[string]string
	}{
		{"configured and derived", []string{"kompass", "/w/beta", "/home/mara/notes"}, []string{"/home/mara/notes"},
			map[string]string{"kompass": "kompass", "/w/beta": "beta", "/home/mara/notes": "~/notes"}},
		{"derived names still disambiguate", []string{"kompass", "/a/x", "/b/x"}, nil,
			map[string]string{"kompass": "kompass", "/a/x": "a/x", "/b/x": "b/x"}},
		{"derived name equal to a configured one shows the path", []string{"iris", "/w/iris", "other"}, nil,
			map[string]string{"iris": "iris", "/w/iris": "/w/iris", "other": "other"}},
		{"configured name absent: derived name as usual", []string{"/w/iris"}, nil,
			map[string]string{"/w/iris": "iris"}},
		{"directory equal to a configured name shows the path", []string{"iris", "/home/mara/iris"}, []string{"/home/mara/iris"},
			map[string]string{"iris": "iris", "/home/mara/iris": "~/iris"}},
		{"nil", nil, nil, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Names(tt.keys, tt.dirs, "/home/mara"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Names(%q) = %v, want %v", tt.keys, got, tt.want)
			}
		})
	}
	var zero Resolver
	if got := zero.Names([]string{"/w/alpha"}, nil, ""); !reflect.DeepEqual(got, map[string]string{"/w/alpha": "alpha"}) {
		t.Errorf("zero resolver Names = %v", got)
	}
}

func TestResolverLookup(t *testing.T) {
	defs := []Definition{mustDef(t, "kompass", "blue", "a"), mustDef(t, "iris", "", "b")}
	r, err := NewResolver(defs, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Definitions(); !reflect.DeepEqual(got, defs) {
		t.Errorf("Definitions = %+v", got)
	}
	if d, ok := r.Definition("kompass"); !ok || d.Name != "kompass" {
		t.Errorf("Definition(kompass) = %+v, %v", d, ok)
	}
	if _, ok := r.Definition("/w/kompass"); ok {
		t.Error("Definition of a raw key found")
	}
	if got := r.Color("kompass"); got != "#4e79a7" {
		t.Errorf("Color(kompass) = %q", got)
	}
	if got := r.Color("iris"); got != "" {
		t.Errorf("Color(iris) = %q, want none", got)
	}
	if got := r.Color("unknown"); got != "" {
		t.Errorf("Color(unknown) = %q, want none", got)
	}
	if r.Empty() {
		t.Error("resolver with definitions is Empty")
	}
	var zero Resolver
	if !zero.Empty() {
		t.Error("zero resolver is not Empty")
	}
}

func TestTargets(t *testing.T) {
	tests := []struct {
		key    string
		dir    bool
		remote string
		want   []string
	}{
		{"/home/u/src/kompass", false, "https://git.invalid/mara/kompass", []string{"/home/u/src/kompass", "~/src/kompass", "kompass", "https://git.invalid/mara/kompass"}},
		{"/home/u/notes", true, "", []string{"/home/u/notes", "~/notes", "~/notes"}},
		{"other", false, "", nil},
	}
	for _, tt := range tests {
		got := Targets(tt.key, tt.dir, tt.remote, "/home/u")
		if !slices.Equal(got, tt.want) {
			t.Errorf("Targets(%q, %v, %q) = %v, want %v", tt.key, tt.dir, tt.remote, got, tt.want)
		}
	}
}

func TestResolverEqual(t *testing.T) {
	resolver := func(home string, defs ...Definition) Resolver {
		r, err := NewResolver(defs, home)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a := resolver("/home/u", mustDef(t, "kompass", "blue", "~/src/kompass"))
	tests := []struct {
		name string
		o    Resolver
		want bool
	}{
		{"same", resolver("/home/u", mustDef(t, "kompass", "blue", "~/src/kompass")), true},
		{"zero", Resolver{}, false},
		{"colour", resolver("/home/u", mustDef(t, "kompass", "green", "~/src/kompass")), false},
		{"pattern", resolver("/home/u", mustDef(t, "kompass", "blue", "~/src/kompass*")), false},
		{"home", resolver("/home/v", mustDef(t, "kompass", "blue", "~/src/kompass")), false},
	}
	for _, tt := range tests {
		if got := a.Equal(tt.o); got != tt.want {
			t.Errorf("%s: Equal = %v, want %v", tt.name, got, tt.want)
		}
	}
	if !(Resolver{}).Equal(Resolver{}) {
		t.Error("two zero resolvers differ")
	}
}
