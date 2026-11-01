package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// env returns a lookup func over a fixed map.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func baseOpts(t *testing.T, goos string, e map[string]string) Options {
	t.Helper()
	return Options{GOOS: goos, Home: "/home/u", Getenv: env(e)}
}

func TestDefaults(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		env        map[string]string
		wantDB     string
		wantConfig string
		wantProj   string
	}{
		{"darwin", "darwin", nil, "/home/u/Library/Application Support/total-recall/archive.db", "/home/u/.config/total-recall/config.toml", "/home/u/.claude/projects"},
		{"linux no xdg", "linux", nil, "/home/u/.local/share/total-recall/archive.db", "/home/u/.config/total-recall/config.toml", "/home/u/.claude/projects"},
		{"linux xdg", "linux", map[string]string{"XDG_DATA_HOME": "/x/data", "XDG_CONFIG_HOME": "/x/cfg"}, "/x/data/total-recall/archive.db", "/x/cfg/total-recall/config.toml", "/home/u/.claude/projects"},
		{"darwin xdg config only", "darwin", map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "XDG_DATA_HOME": "/x/data"}, "/home/u/Library/Application Support/total-recall/archive.db", "/x/cfg/total-recall/config.toml", "/home/u/.claude/projects"},
		{"claude config dir", "linux", map[string]string{"CLAUDE_CONFIG_DIR": "/c"}, "/home/u/.local/share/total-recall/archive.db", "/home/u/.config/total-recall/config.toml", "/c/projects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// config file path points to a non-existent default location: defaults apply.
			c, err := Load(baseOpts(t, tt.goos, tt.env))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if c.DB != tt.wantDB {
				t.Errorf("DB = %q, want %q", c.DB, tt.wantDB)
			}
			if c.Path != tt.wantConfig {
				t.Errorf("Path = %q, want %q", c.Path, tt.wantConfig)
			}
			if c.Harness.Claude.ProjectsDir != tt.wantProj {
				t.Errorf("ProjectsDir = %q, want %q", c.Harness.Claude.ProjectsDir, tt.wantProj)
			}
			if c.Timezone != "Europe/Berlin" || c.Backup.KeepDaily != 7 || c.Backup.KeepMonthly != 0 ||
				c.Backup.MinInterval.Duration != 24*time.Hour || !c.Redact.Enabled ||
				c.Ingest.ToolResultMaxBytes != 4096 || c.Ingest.RawMaxBytes != 16384 || !c.Ingest.StoreRaw || !c.Harness.Claude.Enabled || !c.Git.Resolve {
				t.Errorf("unexpected defaults: %+v", c)
			}
			if c.Backup.Dir != "/home/u/Documents/total-recall" {
				t.Errorf("Backup.Dir = %q", c.Backup.Dir)
			}
			if got := strings.Join(c.Harness.Claude.Actors.AutomationPrefixes, "|"); got != "Hourly |Weekend watch" {
				t.Errorf("automation prefixes = %q", got)
			}
			if c.Origin("db") != OriginDefault {
				t.Errorf("origin db = %v", c.Origin("db"))
			}
		})
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFileAndOrigins(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "c.toml", `
db = "~/x/$PROJ/archive.db"
timezone = "UTC"
[backup]
dir = "${BK}/backups"
keep_daily = 3
min_interval = "12h"
[redact]
extra_patterns = ["foo[0-9]+"]
[harness.claude.actors]
peer_prefixes = ["Peer:"]
`)
	c, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(map[string]string{"PROJ": "p1", "BK": "/b"}), ConfigFile: p})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checks := []struct {
		key    string
		got    any
		want   any
		origin Origin
	}{
		{"db", c.DB, "/home/u/x/p1/archive.db", OriginFile},
		{"timezone", c.Timezone, "UTC", OriginFile},
		{"backup.dir", c.Backup.Dir, "/b/backups", OriginFile},
		{"backup.keep_daily", c.Backup.KeepDaily, 3, OriginFile},
		{"backup.keep_monthly", c.Backup.KeepMonthly, 0, OriginDefault},
		{"backup.min_interval", c.Backup.MinInterval.Duration, 12 * time.Hour, OriginFile},
		{"redact.enabled", c.Redact.Enabled, true, OriginDefault},
		{"harness.claude.actors.peer_prefixes", strings.Join(c.Harness.Claude.Actors.PeerPrefixes, ","), "Peer:", OriginFile},
	}
	for _, ck := range checks {
		if ck.got != ck.want {
			t.Errorf("%s = %v, want %v", ck.key, ck.got, ck.want)
		}
		if o := c.Origin(ck.key); o != ck.origin {
			t.Errorf("origin %s = %v, want %v", ck.key, o, ck.origin)
		}
	}
	if c.PathOrigin != OriginFlag {
		t.Errorf("PathOrigin = %v", c.PathOrigin)
	}
}

func TestPrecedence(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "c.toml", "db = \"/file/db\"\ntimezone = \"UTC\"\n[backup]\ndir = \"/file/bk\"\n")
	tests := []struct {
		name       string
		env        map[string]string
		flags      Flags
		wantDB     string
		wantDBOrig Origin
		wantBK     string
		wantBKOrig Origin
		wantTZ     string
		wantTZOrig Origin
	}{
		{"file only", map[string]string{"TOTAL_RECALL_CONFIG": p}, Flags{}, "/file/db", OriginFile, "/file/bk", OriginFile, "UTC", OriginFile},
		{"env over file", map[string]string{"TOTAL_RECALL_CONFIG": p, "TOTAL_RECALL_DB": "/env/db", "TOTAL_RECALL_BACKUP_DIR": "~/envbk"}, Flags{}, "/env/db", OriginEnv, "/home/u/envbk", OriginEnv, "UTC", OriginFile},
		{"flag over env", map[string]string{"TOTAL_RECALL_CONFIG": p, "TOTAL_RECALL_DB": "/env/db"}, Flags{DB: "/flag/db", BackupDir: "/flag/bk", Timezone: "America/New_York"}, "/flag/db", OriginFlag, "/flag/bk", OriginFlag, "America/New_York", OriginFlag},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(tt.env), Flags: tt.flags})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if c.DB != tt.wantDB || c.Origin("db") != tt.wantDBOrig {
				t.Errorf("db = %q (%v), want %q (%v)", c.DB, c.Origin("db"), tt.wantDB, tt.wantDBOrig)
			}
			if c.Backup.Dir != tt.wantBK || c.Origin("backup.dir") != tt.wantBKOrig {
				t.Errorf("backup.dir = %q (%v), want %q (%v)", c.Backup.Dir, c.Origin("backup.dir"), tt.wantBK, tt.wantBKOrig)
			}
			if c.Timezone != tt.wantTZ || c.Origin("timezone") != tt.wantTZOrig {
				t.Errorf("timezone = %q (%v), want %q (%v)", c.Timezone, c.Origin("timezone"), tt.wantTZ, tt.wantTZOrig)
			}
			if c.PathOrigin != OriginEnv {
				t.Errorf("PathOrigin = %v", c.PathOrigin)
			}
		})
	}
}

func TestConfigFlagOverEnv(t *testing.T) {
	dir := t.TempDir()
	pf := writeFile(t, dir, "flag.toml", "timezone = \"UTC\"\n")
	pe := writeFile(t, dir, "env.toml", "timezone = \"Asia/Tokyo\"\n")
	c, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(map[string]string{"TOTAL_RECALL_CONFIG": pe}), ConfigFile: pf})
	if err != nil {
		t.Fatal(err)
	}
	if c.Timezone != "UTC" || c.Path != pf || c.PathOrigin != OriginFlag {
		t.Errorf("got tz %q path %q origin %v", c.Timezone, c.Path, c.PathOrigin)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"unknown top key", "dbb = \"x\"\n", "dbb"},
		{"unknown nested key", "[backup]\nkeep_dayly = 3\n", "backup.keep_dayly"},
		{"unknown table", "[nope]\nx = 1\n", "nope"},
		{"bad toml", "db = \n", "toml"},
		{"bad timezone", "timezone = \"Mars/Base\"\n", "timezone"},
		{"bad duration", "[backup]\nmin_interval = \"soon\"\n", "min_interval"},
		{"negative keep", "[backup]\nkeep_daily = -1\n", "keep_daily"},
		{"negative raw max", "[ingest]\nraw_max_bytes = -1\n", "raw_max_bytes"},
		{"bad regex", "[redact]\nextra_patterns = [\"(\"]\n", "extra_patterns"},
		{"bad allow regex", "[redact]\nallow = [\"[\"]\n", "allow"},
		{"wrong type", "[git]\nresolve = \"yes\"\n", "resolve"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeFile(t, t.TempDir(), "c.toml", tt.content)
			_, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(nil), ConfigFile: p})
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExplicitConfigMissingIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")
	if _, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(nil), ConfigFile: missing}); err == nil {
		t.Error("expected error for missing --config file")
	}
	if _, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(map[string]string{"TOTAL_RECALL_CONFIG": missing})}); err == nil {
		t.Error("expected error for missing TOTAL_RECALL_CONFIG file")
	}
}

func TestExpandPath(t *testing.T) {
	e := env(map[string]string{"A": "/a", "B": "b"})
	tests := []struct{ in, want string }{
		{"", ""},
		{"~", "/home/u"},
		{"~/x", "/home/u/x"},
		{"$A/x", "/a/x"},
		{"${A}/$B", "/a/b"},
		{"/abs/~/x", "/abs/~/x"},
		{"~other/x", "~other/x"},
		{"$UNSET/x", "/x"},
	}
	for _, tt := range tests {
		if got := ExpandPath(tt.in, "/home/u", e); got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLogPath(t *testing.T) {
	c, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(map[string]string{"TOTAL_RECALL_DB": "/d/archive.db"})})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.LogPath(); got != "/d/total-recall.log" {
		t.Errorf("LogPath = %q", got)
	}
	c.Log = "/var/log/tr.log"
	if got := c.LogPath(); got != "/var/log/tr.log" {
		t.Errorf("LogPath = %q", got)
	}
}

func TestLocation(t *testing.T) {
	c, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if loc := c.Location(); loc.String() != "Europe/Berlin" {
		t.Errorf("Location = %v", loc)
	}
}

func TestEntries(t *testing.T) {
	c, err := Load(Options{GOOS: "linux", Home: "/home/u", Getenv: env(map[string]string{"TOTAL_RECALL_DB": "/env/db"})})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range c.Entries() {
		got[e.Key] = e
	}
	for _, k := range []string{"db", "timezone", "log", "backup.dir", "backup.keep_daily", "backup.keep_monthly", "backup.min_interval",
		"redact.enabled", "redact.extra_patterns", "redact.allow", "ingest.tool_result_max_bytes", "ingest.raw_max_bytes", "ingest.store_raw",
		"harness.claude.enabled", "harness.claude.projects_dir", "harness.claude.skip_record_types",
		"harness.claude.actors.peer_prefixes", "harness.claude.actors.automation_prefixes", "git.resolve"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing entry %q", k)
		}
	}
	if e := got["db"]; e.Value != "/env/db" || e.Origin != OriginEnv {
		t.Errorf("db entry = %+v", e)
	}
	if e := got["backup.min_interval"]; e.Value != "24h0m0s" {
		t.Errorf("min_interval entry = %+v", e)
	}
	if e := got["harness.claude.actors.automation_prefixes"]; e.Value != `["Hourly ", "Weekend watch"]` {
		t.Errorf("automation entry = %+v", e)
	}
}

func TestTemplateParsesToDefaults(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "c.toml", Template)
	for _, goos := range []string{"darwin", "linux"} {
		def, err := Load(Options{GOOS: goos, Home: "/home/u", Getenv: env(nil)})
		if err != nil {
			t.Fatal(err)
		}
		fromTpl, err := Load(Options{GOOS: goos, Home: "/home/u", Getenv: env(nil), ConfigFile: p})
		if err != nil {
			t.Fatalf("template does not load: %v", err)
		}
		de, te := def.Entries(), fromTpl.Entries()
		for i := range de {
			if de[i].Key != te[i].Key || de[i].Value != te[i].Value {
				t.Errorf("%s: template %s = %q, default %q", goos, te[i].Key, te[i].Value, de[i].Value)
			}
		}
	}
}

func TestWriteTemplate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "config.toml")
	if err := WriteTemplate(p); err != nil {
		t.Fatalf("WriteTemplate: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != Template {
		t.Fatalf("content mismatch: %v", err)
	}
	if err := os.WriteFile(p, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = WriteTemplate(p)
	if err == nil || !os.IsExist(err) {
		t.Fatalf("expected exist error, got %v", err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != "mine" {
		t.Error("existing file was overwritten")
	}
}
