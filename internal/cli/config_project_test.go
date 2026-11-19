package cli

import (
	"os"
	"strings"
	"testing"
)

func cfgText(t *testing.T, e *testEnv) string {
	t.Helper()
	b, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func projectNames(p []ProjectJSON) string {
	n := make([]string, len(p))
	for i, x := range p {
		n[i] = x.Name
	}
	return strings.Join(n, ",")
}

func TestConfigProject(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.mustRun(t, "config", "set", "tui.view", "stats") // something with a comment-free file to keep
	run := func(args ...string) result {
		return e.run(t, "", append([]string{"config", "project"}, append(args, "--json")...)...)
	}
	must := func(args ...string) ConfigProjectJSON {
		t.Helper()
		r := run(args...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
		raw := decode[map[string]any](t, r.stdout)
		for _, k := range []string{"file", "action", "changed", "project", "projects"} {
			if _, ok := raw[k]; !ok {
				t.Fatalf("%v: key %q missing: %s", args, k, r.stdout)
			}
		}
		return decode[ConfigProjectJSON](t, r.stdout)
	}

	o := must("add", "kompass", "--match", "~/src/kompass*", "--match", "https://git.invalid/mara/kompass", "--description", "Wanderkarten", "--color", "blue")
	if o.Action != "add" || !o.Changed || o.Project.Name != "kompass" || o.Project.Position != 1 || o.Project.Description != "Wanderkarten" || o.Project.Color != "blue" || len(o.Project.Match) != 2 {
		t.Errorf("add = %+v", o)
	}
	must("add", "wetterwarte", "--match", "~/src/wetter")
	o = must("add", "fahrtenbuch", "--match", "~/src/fahrten")
	if projectNames(o.Projects) != "kompass,wetterwarte,fahrtenbuch" || o.Projects[2].Position != 3 {
		t.Errorf("projects = %+v", o.Projects)
	}
	if o.Projects[1].Description != "" || o.Projects[1].Color != "" {
		t.Errorf("empty fields = %+v", o.Projects[1])
	}

	// set changes the given fields only.
	o = must("set", "wetterwarte", "--description", "Vorhersage", "--match", "~/src/wetter", "--match", "~/src/wolken")
	if o.Action != "set" || o.Project.Description != "Vorhersage" || len(o.Project.Match) != 2 || o.Project.Name != "wetterwarte" {
		t.Errorf("set = %+v", o)
	}
	o = must("set", "wetterwarte", "--name", "wetter", "--color", "#112233")
	if o.Project.Name != "wetter" || o.Project.Description != "Vorhersage" || o.Project.Color != "#112233" || len(o.Project.Match) != 2 {
		t.Errorf("rename = %+v", o)
	}
	o = must("set", "wetter", "--description", "", "--color", "")
	if o.Project.Description != "" || o.Project.Color != "" {
		t.Errorf("cleared = %+v", o)
	}

	// move: position is 1-based.
	o = must("move", "fahrtenbuch", "--to", "1")
	if projectNames(o.Projects) != "fahrtenbuch,kompass,wetter" || o.Project.Position != 1 {
		t.Errorf("move = %+v", o)
	}
	if o := must("move", "fahrtenbuch", "--to", "1"); o.Changed {
		t.Errorf("no-op move reports changed: %+v", o)
	}

	// show --json agrees with what the commands report (#323 reads the same).
	o = must("rm", "kompass")
	if o.Action != "rm" || o.Project.Name != "kompass" || projectNames(o.Projects) != "fahrtenbuch,wetter" {
		t.Errorf("rm = %+v", o)
	}
	b, _ := os.ReadFile(e.configPath)
	if !strings.Contains(string(b), `view = "stats"`) || strings.Contains(string(b), "kompass") {
		t.Errorf("file:\n%s", b)
	}
}

func TestConfigProjectErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		kind string
	}{
		{"add without pattern", []string{"add", "kompass"}, ErrKindValidation},
		{"add empty pattern", []string{"add", "kompass", "--match", ""}, ErrKindValidation},
		{"add bad colour", []string{"add", "kompass", "--match", "x", "--color", "puce"}, ErrKindValidation},
		{"add duplicate", []string{"add", "kompass", "--match", "y"}, ErrKindValidation},
		{"add without name", []string{"add", "--match", "x"}, ErrKindUsage},
		{"set unknown", []string{"set", "nope", "--description", "x"}, ErrKindNotFound},
		{"set nothing", []string{"set", "kompass"}, ErrKindUsage},
		{"set rename to duplicate", []string{"set", "kompass", "--name", "wetter"}, ErrKindValidation},
		{"rm unknown", []string{"rm", "nope"}, ErrKindNotFound},
		{"move unknown", []string{"move", "nope", "--to", "1"}, ErrKindNotFound},
		{"move out of range", []string{"move", "kompass", "--to", "5"}, ErrKindValidation},
		{"move without --to", []string{"move", "kompass"}, ErrKindUsage},
		{"unknown action", []string{"frob"}, ErrKindUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEnv(t)
			e.mustRun(t, "config", "project", "add", "kompass", "--match", "x")
			e.mustRun(t, "config", "project", "add", "wetter", "--match", "w")
			before := cfgText(t, e)
			r := e.run(t, "", append([]string{"config", "project"}, append(tt.args, "--json")...)...)
			if r.code != 1 {
				t.Fatalf("exit %d: %+v", r.code, r)
			}
			if got := decode[ErrorJSON](t, r.stdout).Error.Kind; got != tt.kind {
				t.Errorf("kind = %q, want %q (%s)", got, tt.kind, r.stdout)
			}
			if cfgText(t, e) != before {
				t.Error("the file changed")
			}
		})
	}
}

func TestConfigProjectTable(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	out := e.mustRun(t, "config", "project", "add", "kompass", "--match", "~/k")
	if !strings.Contains(out, "kompass") || !strings.Contains(out, "~/k") {
		t.Errorf("table = %q", out)
	}
}

// TestConfigShowCatalogAndProjects checks that config show --json carries
// the catalog of the keys (kind, allowed values, default, help) and the
// [[project]] definitions as structured data, including the description.
func TestConfigShowCatalogAndProjects(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.mustRun(t, "config", "project", "add", "kompass", "--match", "~/src/kompass*", "--match", "https://git.invalid/mara/kompass", "--description", "Wanderkarten", "--color", "blue")
	e.mustRun(t, "config", "project", "add", "wetterwarte", "--match", "~/src/wetter")
	e.mustRun(t, "config", "set", "tui.view", "stats")

	raw := decode[map[string]any](t, e.mustRun(t, "config", "show", "--json"))
	for _, k := range []string{"catalog", "projects"} {
		if _, ok := raw[k].([]any); !ok {
			t.Fatalf("%q is no list: %#v", k, raw[k])
		}
	}
	c := decode[ConfigJSON](t, e.mustRun(t, "config", "show", "--json"))

	if len(c.Projects) != 2 || c.Projects[0].Name != "kompass" || c.Projects[0].Description != "Wanderkarten" || c.Projects[0].Color != "blue" || len(c.Projects[0].Match) != 2 || c.Projects[1].Position != 2 || c.Projects[1].Description != "" {
		t.Errorf("projects = %+v", c.Projects)
	}

	byKey := map[string]ConfigKeyJSON{}
	for _, k := range c.Catalog {
		byKey[k.Key] = k
	}
	values := map[string]ConfigValue{}
	for _, v := range c.Values {
		values[v.Key] = v
	}
	// Every key of the catalog is listed once, with its kind, and the
	// catalog and the values agree: a key with a value has a catalog entry
	// and the other way round (tables are shown as one text).
	if len(byKey) != len(c.Catalog) {
		t.Errorf("catalog has duplicate keys")
	}
	for _, k := range c.Catalog {
		if k.Kind == "" || k.Help == "" || k.Enum == nil {
			t.Errorf("catalog entry incomplete: %+v", k)
		}
		if _, ok := values[k.Key]; !ok && k.Kind != "table" {
			t.Errorf("catalog key %q has no value", k.Key)
		}
	}
	for key := range values {
		if _, ok := byKey[key]; !ok && key != "log (effective)" {
			t.Errorf("value %q has no catalog entry", key)
		}
	}
	view := byKey["tui.view"]
	if view.Kind != "enum" || len(view.Enum) < 3 || view.Default != "search" || view.Help == "" {
		t.Errorf("tui.view = %+v", view)
	}
	if k := byKey["tui.mouse"]; k.Kind != "bool" || len(k.Enum) != 0 || k.Default != "true" {
		t.Errorf("tui.mouse = %+v", k)
	}
	if k := byKey["project"]; k.Kind != "table" || k.Default != "" {
		t.Errorf("project = %+v", k)
	}
	if k, ok := byKey["keys.global_quit"]; !ok || k.Kind != "list" || k.Default == "" {
		t.Errorf("keys.global_quit = %+v (%v)", k, ok)
	}
	if values["tui.view"].Origin != "file" || values["tui.view"].Value != "stats" {
		t.Errorf("value = %+v", values["tui.view"])
	}
}
