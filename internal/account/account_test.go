package account

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	t.Parallel()
	r := New([]Entry{
		{ID: "a1", Organization: "o1", Name: "kompass-team", Group: "firma"},
		{ID: "a1", Name: "stefan", Group: "alle"},
		{Organization: "o9", Name: "wetterwarte", Group: "privat"},
	}, []Entry{
		{ID: "a2", Organization: "o2", Name: "from-registry", Group: "reg"},
		{ID: "a1", Organization: "o1", Name: "registry-loses", Group: "registry-loses"},
	})
	tests := []struct {
		name         string
		acc, org     string
		wantName     string
		wantGroup    string
		wantOrgName  string
		wantKey      string
		wantKnownKey bool
	}{
		{"pair beats account", "a1", "o1", "kompass-team", "firma", "", "a1/o1", true},
		{"account only entry for other org", "a1", "o3", "stefan", "alle", "", "a1/o3", true},
		{"org only entry", "a7", "o9", "wetterwarte", "privat", "wetterwarte", "a7/o9", true},
		{"registry pair", "a2", "o2", "from-registry", "reg", "", "a2/o2", true},
		{"unknown", "zz", "yy", "", "", "", "zz/yy", true},
		{"no org", "a1", "", "stefan", "alle", "", "a1", true},
		{"nothing", "", "", "", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := r.Resolve(tt.acc, tt.org)
			if id.Name != tt.wantName || id.Group != tt.wantGroup || id.OrganizationName != tt.wantOrgName {
				t.Errorf("Resolve(%q, %q) = %+v", tt.acc, tt.org, id)
			}
			if got := Key(tt.acc, tt.org); got != tt.wantKey {
				t.Errorf("Key = %q, want %q", got, tt.wantKey)
			}
			a, o := SplitKey(Key(tt.acc, tt.org))
			if a != tt.acc || o != tt.org {
				t.Errorf("SplitKey(Key) = %q, %q", a, o)
			}
		})
	}
	if got, want := r.Groups(), []string{"alle", "firma", "privat", "reg", "registry-loses"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Groups = %v, want %v", got, want)
	}
}

func TestConfigBeatsRegistryAcrossTiers(t *testing.T) {
	t.Parallel()
	r := New([]Entry{{ID: "a1", Name: "from-config"}}, []Entry{{ID: "a1", Organization: "o1", Name: "from-registry"}})
	if got := r.Resolve("a1", "o1").Name; got != "from-config" {
		t.Errorf("Name = %q, want the config entry even though the registry entry is a pair", got)
	}
}

func TestNilResolver(t *testing.T) {
	t.Parallel()
	var r *Resolver
	if id := r.Resolve("a", "o"); id != (Identity{}) {
		t.Errorf("Resolve = %+v", id)
	}
	if g := r.Groups(); len(g) != 0 {
		t.Errorf("Groups = %v", g)
	}
}

func TestParseLegacyKey(t *testing.T) {
	t.Parallel()
	got := FromNames(map[string]string{"a1": "alice", "a1/o1": "pair", "o2": "org"})
	r := New(got, nil)
	if n := r.Resolve("a1", "o1").Name; n != "pair" {
		t.Errorf("pair key: %q", n)
	}
	if n := r.Resolve("a1", "o9").Name; n != "alice" {
		t.Errorf("account key: %q", n)
	}
	// legacy [accounts] mixes account and organization IDs: an ID that is
	// only an organization names the organization and, lacking others, the pair
	if id := r.Resolve("a5", "o2"); id.Name != "org" || id.OrganizationName != "org" {
		t.Errorf("org key: %+v", id)
	}
}

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "team", ".claude.json"), `{"oauthAccount":{"accountUuid":"a1","organizationUuid":"o1"},"other":1}`)
	writeJSON(t, filepath.Join(dir, "solo", ".claude.json"), `{"oauthAccount":{"accountUuid":"a1","organizationUuid":"o2"}}`)
	writeJSON(t, filepath.Join(dir, "broken", ".claude.json"), `{not json`)
	reg := filepath.Join(dir, "registry.json")
	writeJSON(t, reg, `{"accounts":{
	  "kompass-team":{"group":"firma","label":"Kompass Team","short":"kt","dir":"@/team"},
	  "kompass-solo":{"group":"firma","dir":"@/solo"},
	  "missing":{"group":"x","dir":"@/nowhere"},
	  "broken":{"group":"x","dir":"@/broken"},
	  "nodir":{"group":"x"}}}`)
	expand := func(s string) string { return strings.Replace(s, "@", dir, 1) }
	got, warns, err := LoadRegistry(reg, expand)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{ID: "a1", Organization: "o2", Name: "kompass-solo", Group: "firma"},
		{ID: "a1", Organization: "o1", Name: "kompass-team", Group: "firma"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %+v, want %+v", got, want)
	}
	if len(warns) != 3 {
		t.Errorf("warnings = %v, want 3 (missing, broken, nodir)", warns)
	}
	for _, w := range warns {
		if strings.Contains(w, dir) {
			t.Errorf("warning leaks the full path: %q", w)
		}
	}
}

func TestLoadRegistryErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	writeJSON(t, bad, `{"accounts": [1]}`)
	for _, path := range []string{filepath.Join(dir, "none.json"), bad} {
		if _, _, err := LoadRegistry(path, func(s string) string { return s }); err == nil {
			t.Errorf("LoadRegistry(%s): no error", path)
		}
	}
}
