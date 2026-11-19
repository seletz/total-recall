package cli

import (
	"os"
	"strings"
	"testing"
)

// readConfig returns the config file of the test installation.
func (e *testEnv) readConfig(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConfigSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		args        []string
		wantKind    string // error kind; "" = success
		wantValue   string
		wantOrigin  string
		wantChanged bool
	}{
		{"enum", []string{"tui.view", "stats"}, "", "stats", "file", true},
		{"bool", []string{"tui.mouse", "false"}, "", "false", "file", true},
		{"duration", []string{"tui.reload", "10s"}, "", "10s", "file", true},
		{"list", []string{"redact.allow", "foo, bar"}, "", `["foo", "bar"]`, "file", true},
		{"same value again", []string{"timezone", "Europe/Berlin"}, "", "Europe/Berlin", "file", false},
		{"unknown key", []string{"nope", "x"}, ErrKindValidation, "", "", false},
		{"bad enum", []string{"tui.view", "nope"}, ErrKindValidation, "", "", false},
		{"bad bool", []string{"tui.mouse", "maybe"}, ErrKindValidation, "", "", false},
		{"table", []string{"project", "x"}, ErrKindValidation, "", "", false},
		{"missing value", []string{"tui.view"}, ErrKindUsage, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEnv(t)
			before := e.readConfig(t)
			r := e.run(t, "", append([]string{"config", "set", "--json"}, tt.args...)...)
			if tt.wantKind != "" {
				if r.code != 1 {
					t.Fatalf("exit %d, want 1: %+v", r.code, r)
				}
				if got := decode[ErrorJSON](t, r.stdout).Error.Kind; got != tt.wantKind {
					t.Errorf("kind = %q, want %q (%s)", got, tt.wantKind, r.stdout)
				}
				if e.readConfig(t) != before {
					t.Error("the file changed")
				}
				return
			}
			if r.code != 0 {
				t.Fatalf("exit %d: %+v", r.code, r)
			}
			raw := decode[map[string]any](t, r.stdout)
			for _, k := range []string{"file", "key", "value", "origin", "default", "changed"} {
				if _, ok := raw[k]; !ok {
					t.Errorf("key %q missing: %s", k, r.stdout)
				}
			}
			got := decode[ConfigChangeJSON](t, r.stdout)
			if got.File != e.configPath || got.Key != tt.args[0] || string(got.Value) != tt.wantValue || got.Origin != tt.wantOrigin || got.Changed != tt.wantChanged {
				t.Errorf("result = %+v", got)
			}
			// The archive is never touched by a config edit.
			if _, err := os.Stat(e.db); err == nil {
				t.Error("config set created the archive")
			}
		})
	}
}

func TestConfigSetKeepsCommentsAndUnsetRestoresDefault(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	if err := os.WriteFile(e.configPath, []byte("# my notes\n"+e.readConfig(t)+"\n# trailing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.mustRun(t, "config", "set", "tui.view", "stats")
	f := e.readConfig(t)
	if !strings.Contains(f, "# my notes") || !strings.Contains(f, "# trailing") || !strings.Contains(f, `view = "stats"`) {
		t.Errorf("file after set:\n%s", f)
	}
	if !strings.Contains(f, "timezone = \"Europe/Berlin\"") {
		t.Errorf("other keys lost:\n%s", f)
	}

	r := decode[ConfigChangeJSON](t, e.mustRun(t, "config", "unset", "tui.view", "--json"))
	if !r.Changed || r.Origin != "default" || string(r.Value) != string(r.Default) || r.Default == "" {
		t.Errorf("unset = %+v", r)
	}
	if strings.Contains(e.readConfig(t), "\nview = \"stats\"") ||!strings.Contains(e.readConfig(t), "# my notes") {
		t.Errorf("file after unset:\n%s", e.readConfig(t))
	}
	// Unsetting what the file does not set changes nothing.
	r = decode[ConfigChangeJSON](t, e.mustRun(t, "config", "unset", "tui.view", "--json"))
	if r.Changed {
		t.Errorf("second unset = %+v", r)
	}
	if r := e.run(t, "", "config", "unset", "nope", "--json"); r.code != 1 || decode[ErrorJSON](t, r.stdout).Error.Kind != ErrKindValidation {
		t.Errorf("unset unknown = %+v", r)
	}
}

func TestConfigSetTable(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	out := e.mustRun(t, "config", "set", "tui.view", "stats")
	if !strings.Contains(out, "tui.view") || !strings.Contains(out, "stats") {
		t.Errorf("table output = %q", out)
	}
}
