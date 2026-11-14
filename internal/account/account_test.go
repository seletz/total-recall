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
	got, warns, err := LoadRegistry(reg, "", expand)
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
		if _, _, err := LoadRegistry(path, "", func(s string) string { return s }); err == nil {
			t.Errorf("LoadRegistry(%s): no error", path)
		}
	}
}

// TestLoadRegistryDefaultDir: without CLAUDE_CONFIG_DIR the harness keeps
// the login in $HOME/.claude.json, not in $HOME/.claude/.claude.json (where a
// stale file may lie); other directories hold their own .claude.json.
func TestLoadRegistryDefaultDir(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeJSON(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"accountUuid":"a1","organizationUuid":"o-real"}}`)
	writeJSON(t, filepath.Join(home, ".claude", ".claude.json"), `{"oauthAccount":{"accountUuid":"a1","organizationUuid":"o-stale"}}`)
	writeJSON(t, filepath.Join(home, "other", ".claude.json"), `{"oauthAccount":{"accountUuid":"a2","organizationUuid":"o2"}}`)
	for name, dir := range map[string]string{"plain": "~/.claude", "slash": "~/.claude/", "dots": "~/x/../.claude"} {
		reg := filepath.Join(home, name+".json")
		writeJSON(t, reg, `{"accounts":{"default":{"group":"g","dir":"`+dir+`"}}}`)
		expand := func(s string) string { return strings.Replace(s, "~", home, 1) }
		got, _, err := LoadRegistry(reg, home, expand)
		if err != nil || len(got) != 1 || got[0].Organization != "o-real" {
			t.Errorf("%s: entries %+v, err %v; want the login of $HOME/.claude.json", name, got, err)
		}
	}
	reg := filepath.Join(home, "o.json")
	writeJSON(t, reg, `{"accounts":{"other":{"dir":"`+filepath.Join(home, "other")+`"}}}`)
	got, _, err := LoadRegistry(reg, home, nil)
	if err != nil || len(got) != 1 || got[0].Organization != "o2" {
		t.Errorf("other dir: %+v, %v", got, err)
	}
}

// TestLoadRegistryCollision: two entries with the same login are reported
// and the first by name wins; the same account in different organizations is
// no collision.
func TestLoadRegistryCollision(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, org := range map[string]string{"a": "o1", "b": "o1", "c": "o2"} {
		writeJSON(t, filepath.Join(dir, name, ".claude.json"), `{"oauthAccount":{"accountUuid":"acc","organizationUuid":"`+org+`"}}`)
	}
	reg := filepath.Join(dir, "r.json")
	writeJSON(t, reg, `{"accounts":{"zeta":{"dir":"`+filepath.Join(dir, "b")+`"},"alpha":{"dir":"`+filepath.Join(dir, "a")+`"},"mid":{"dir":"`+filepath.Join(dir, "c")+`"}}}`)
	got, warns, err := LoadRegistry(reg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "alpha,mid" {
		t.Errorf("entries = %v, want alpha and mid (zeta collides with alpha)", names)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "alpha") || !strings.Contains(warns[0], "zeta") || strings.Contains(warns[0], dir) {
		t.Errorf("warnings = %v", warns)
	}
}

// TestLoadRegistryOrganizationName: organizationName of the .claude.json
// names the organization, below every other name.
func TestLoadRegistryOrganizationName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "t", ".claude.json"), `{"oauthAccount":{"accountUuid":"a1","organizationUuid":"o1","organizationName":"Kompass GmbH"}}`)
	reg := filepath.Join(dir, "r.json")
	writeJSON(t, reg, `{"accounts":{"kompass-team":{"group":"firma","dir":"`+filepath.Join(dir, "t")+`"}}}`)
	got, _, err := LoadRegistry(reg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := New([]Entry{{Organization: "o9", Name: "fremd"}}, got)
	if id := r.Resolve("a1", "o1"); id.Name != "kompass-team" || id.OrganizationName != "Kompass GmbH" || id.Group != "firma" {
		t.Errorf("Resolve = %+v", id)
	}
	// a configured organization name wins
	r = New([]Entry{{Organization: "o1", Name: "kompass-org"}}, got)
	if id := r.Resolve("a1", "o1"); id.OrganizationName != "kompass-org" || id.Name != "kompass-org" {
		t.Errorf("configured: %+v", id)
	}
}
