package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// editEnv is a config loaded from a temp copy of the template (or no file).
func editEnv(t *testing.T, content string, env map[string]string) *Config {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "total-recall")
	if content != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "config.toml", content)
	}
	// The default location under XDG_CONFIG_HOME, so a missing file is no error.
	c, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return filepath.Dir(dir)
		}
		return env[k]
	}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSetValue(t *testing.T) {
	tests := []struct {
		name, key, text string
		wantLine        string // in the file afterwards
		check           func(t *testing.T, c *Config)
	}{
		{"enum", "tui.view", "stats", `view = "stats"`, func(t *testing.T, c *Config) {
			if c.TUI.View != "stats" || c.Origin("tui.view") != OriginFile {
				t.Errorf("view = %q (%s)", c.TUI.View, c.Origin("tui.view"))
			}
		}},
		{"bool", "tui.mouse", "false", `mouse = false`, func(t *testing.T, c *Config) {
			if c.TUI.Mouse {
				t.Error("mouse still on")
			}
		}},
		{"int", "backup.keep_daily", "3", `keep_daily = 3`, func(t *testing.T, c *Config) {
			if c.Backup.KeepDaily != 3 {
				t.Errorf("keep_daily = %d", c.Backup.KeepDaily)
			}
		}},
		{"duration is written short", "tui.reload", "10s", `reload = "10s"`, func(t *testing.T, c *Config) {
			if c.TUI.Reload.Duration != 10*time.Second {
				t.Errorf("reload = %s", c.TUI.Reload)
			}
		}},
		{"duration hours", "backup.min_interval", "48h", `min_interval = "48h"`, nil},
		{"duration zero", "worktime.grid", "0", `grid = "0"`, func(t *testing.T, c *Config) {
			if c.Worktime.Grid.Duration != 0 {
				t.Errorf("grid = %s", c.Worktime.Grid)
			}
		}},
		{"list from words", "redact.allow", "foo, bar", `allow = ["foo", "bar"]`, func(t *testing.T, c *Config) {
			if strings.Join(c.Redact.Allow, "|") != "foo|bar" {
				t.Errorf("allow = %v", c.Redact.Allow)
			}
		}},
		{"list as toml", "harness.claude.actors.peer_prefixes", `["# Task:"]`, `peer_prefixes = ["# Task:"]`, nil},
		{"empty list", "redact.extra_patterns", "", `extra_patterns = []`, nil},
		{"path activates the commented line", "db", "~/data/archive.db", `db = "~/data/archive.db"`, func(t *testing.T, c *Config) {
			if c.DB != "/home/u/data/archive.db" {
				t.Errorf("db = %q", c.DB)
			}
		}},
		{"key binding activates the example", "keys.global_quit", "q, ctrl+q", `global_quit = ["q", "ctrl+q"]`, func(t *testing.T, c *Config) {
			if strings.Join(c.Keys["global_quit"], "|") != "q|ctrl+q" {
				t.Errorf("keys = %v", c.Keys)
			}
		}},
		{"new key binding is inserted", "keys.stats_earlier", "<", `stats_earlier = ["<"]`, nil},
		{"clock", "day_start", "05:00", `day_start = "05:00"`, nil},
		{"accounts entry", "accounts.aaaaaaaa-0000-4000-8000-000000000002", "private", `aaaaaaaa-0000-4000-8000-000000000002 = "private"`, func(t *testing.T, c *Config) {
			if c.AccountName("aaaaaaaa-0000-4000-8000-000000000002") != "private" {
				t.Errorf("accounts = %v", c.Accounts)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := editEnv(t, Template, nil)
			n, err := c.SetValue(tt.key, tt.text)
			if err != nil {
				t.Fatalf("SetValue: %v", err)
			}
			b, err := os.ReadFile(c.Path)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if !strings.Contains(got, "\n"+tt.wantLine+"\n") {
				t.Errorf("file lacks %q:\n%s", tt.wantLine, got)
			}
			// The comments of the template are still there.
			if strings.Count(got, "\n#") < strings.Count(Template, "\n#")-1 {
				t.Errorf("comments lost: %d, template %d", strings.Count(got, "\n#"), strings.Count(Template, "\n#"))
			}
			if _, err := os.Stat(c.Path + ".bak"); err != nil {
				t.Errorf("no backup: %v", err)
			}
			if n.Path != c.Path || !n.Loaded {
				t.Errorf("returned config: path %q loaded %v", n.Path, n.Loaded)
			}
			if tt.check != nil {
				tt.check(t, n)
			}
		})
	}
}

func TestSetValueRejects(t *testing.T) {
	tests := []struct {
		name, key, text, wantErr string
	}{
		{"enum", "tui.view", "foo", "tui.view"},
		{"reload too short", "tui.reload", "500ms", "reload"},
		{"bad duration", "tui.reload", "soon", "duration"},
		{"negative int", "backup.keep_daily", "-1", "keep_daily"},
		{"not an int", "backup.keep_daily", "many", "integer"},
		{"not a bool", "tui.mouse", "yes", "true or false"},
		{"bad regexp", "redact.extra_patterns", "(", "extra_patterns"},
		{"bad key name", "keys.global_quit", "ctrl+c", "ctrl+c"},
		{"table", "accounts", `"a" = "b"`, "in the file"},
		{"table array", "worktime.category", "x", "in the file"},
		{"unknown key", "nope", "1", "unknown"},
		{"core clock", "day_start", "25:00", "day_start"},
		{"work hours", "work.hours", "9-17", "work.hours"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := editEnv(t, Template, nil)
			_, err := c.SetValue(tt.key, tt.text)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("SetValue(%s, %q) = %v, want an error mentioning %q", tt.key, tt.text, err, tt.wantErr)
			}
			b, _ := os.ReadFile(c.Path)
			if string(b) != Template {
				t.Error("file changed although the value was rejected")
			}
			if _, err := os.Stat(c.Path + ".bak"); err == nil {
				t.Error("a backup was written although nothing changed")
			}
		})
	}
}

func TestSetValueCreatesFile(t *testing.T) {
	c := editEnv(t, "", nil)
	if c.Loaded {
		t.Fatal("loaded without a file")
	}
	n, err := c.SetValue("tui.view", "activity")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# total-recall configuration") || !strings.Contains(string(b), "\nview = \"activity\"\n") {
		t.Errorf("file:\n%s", b)
	}
	if _, err := os.Stat(c.Path + ".bak"); err == nil {
		t.Error("backup of a file that did not exist")
	}
	if !n.Loaded || n.TUI.View != "activity" {
		t.Errorf("config = loaded %v view %q", n.Loaded, n.TUI.View)
	}
}

func TestSetValueKeepsOtherProblems(t *testing.T) {
	// A problem elsewhere in the file (checked only when used) must not
	// block an edit, but the edit must not add one.
	c := editEnv(t, "[work]\nhours = \"nonsense\"\n[tui]\nview = \"search\"\n", nil)
	if len(c.Problems()) != 1 {
		t.Fatalf("problems = %v", c.Problems())
	}
	n, err := c.SetValue("tui.view", "stats")
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if n.TUI.View != "stats" || len(n.Problems()) != 1 {
		t.Errorf("view %q, problems %v", n.TUI.View, n.Problems())
	}
	if _, err := n.SetValue("tui.graphics", "lcd"); err == nil {
		t.Error("new problem accepted")
	}
}

func TestSetValueUnderOverride(t *testing.T) {
	c := editEnv(t, Template, map[string]string{EnvDB: "/env/db"})
	n, err := c.SetValue("db", "/file/db")
	if err != nil {
		t.Fatal(err)
	}
	if n.DB != "/env/db" || n.Origin("db") != OriginEnv {
		t.Errorf("db = %q (%s): the environment must still win", n.DB, n.Origin("db"))
	}
	b, _ := os.ReadFile(c.Path)
	if !strings.Contains(string(b), "\ndb = \"/file/db\"\n") {
		t.Errorf("file not written:\n%s", b)
	}
}

func TestReset(t *testing.T) {
	c := editEnv(t, Template, nil)
	n, err := c.SetValue("tui.view", "stats")
	if err != nil {
		t.Fatal(err)
	}
	n, err = n.Reset("tui.view")
	if err != nil {
		t.Fatal(err)
	}
	if n.TUI.View != "search" || n.Origin("tui.view") != OriginDefault {
		t.Errorf("view = %q (%s)", n.TUI.View, n.Origin("tui.view"))
	}
	b, _ := os.ReadFile(c.Path)
	if !strings.Contains(string(b), "\n# view = \"stats\"\n") {
		t.Errorf("line not commented out:\n%s", b)
	}
	// A key that is not set in the file: nothing to do.
	m, err := n.Reset("tui.graphics")
	if err != nil || m.TUI.Graphics != "auto" {
		t.Errorf("Reset of a default = %v, %v", m, err)
	}
	if _, err := n.Reset("accounts"); err == nil {
		t.Error("Reset of a table succeeded")
	}
}

func TestDefault(t *testing.T) {
	c := editEnv(t, "[tui]\nview = \"stats\"\n[keys]\nglobal_quit = \"x\"\n", nil)
	for key, want := range map[string]string{
		"tui.view": "search", "backup.keep_daily": "7", "tui.reload": "5s", "keys.global_quit": `["q"]`,
		"db": "/home/u/.local/share/total-recall/archive.db", "work.bundesland": "Baden-Württemberg", "accounts": "[]",
	} {
		if got := c.Default(key); got != want {
			t.Errorf("Default(%s) = %q, want %q", key, got, want)
		}
	}
	if got := c.Default("nope"); got != "" {
		t.Errorf("Default(nope) = %q", got)
	}
}

func TestCatalog(t *testing.T) {
	inCatalog := map[string]bool{}
	for _, k := range Catalog {
		if k.Help == "" {
			t.Errorf("%s has no help", k.Name)
		}
		if k.Kind == KindEnum && len(k.Enum) == 0 {
			t.Errorf("%s is an enum without values", k.Name)
		}
		inCatalog[k.Name] = true
	}
	for _, f := range fields {
		if !inCatalog[f.key] {
			t.Errorf("%s is not in the catalog", f.key)
		}
		delete(inCatalog, f.key)
	}
	for k := range inCatalog {
		t.Errorf("%s is in the catalog but not a field", k)
	}
	for name, want := range map[string]Kind{
		"tui.view": KindEnum, "tui.mouse": KindBool, "backup.keep_daily": KindInt, "tui.reload": KindDuration,
		"redact.allow": KindList, "db": KindPath, "day_start": KindClock, "timezone": KindString,
		"accounts": KindTable, "worktime.category": KindTable, "keys.global_quit": KindList, "accounts.some-id": KindString,
	} {
		k, ok := Lookup(name)
		if !ok || k.Kind != want {
			t.Errorf("Lookup(%s) = %+v, %v; want kind %s", name, k, ok, want)
		}
	}
	if _, ok := Lookup("keys.nope"); ok {
		t.Error("Lookup of an unknown action succeeded")
	}
	if k, _ := Lookup("work.bundesland"); len(k.Enum) < 16 {
		t.Errorf("bundesland enum = %v", k.Enum)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0", 5 * time.Second: "5s", 24 * time.Hour: "24h", 90 * time.Minute: "1h30m", 20 * time.Minute: "20m", 1500 * time.Millisecond: "1.5s"} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
