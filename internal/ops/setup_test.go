package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/config"
)

// setupEnv is an installation in a temp home: a config file, and what the
// test creates of the rest.
func setupEnv(t *testing.T) (SetupEnv, string) {
	t.Helper()
	home := t.TempDir()
	cfgPath := filepath.Join(home, "config.toml")
	body := `db = "` + filepath.Join(home, "archive.db") + `"
[backup]
dir = "` + filepath.Join(home, "backups") + `"
[harness.claude]
projects_dir = "` + filepath.Join(home, ".claude", "projects") + `"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{ConfigFile: cfgPath, Home: home, Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin", "total-recall")
	return SetupEnv{
		Config: cfg, Home: home, Version: "1.2.3", ProjectDir: filepath.Join(home, "proj"),
		LookPath:   func(string) (string, error) { return bin, nil },
		Executable: func() (string, error) { return bin, nil },
		VersionOf:  func(string) (string, error) { return "1.2.3", nil },
	}, home
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func byName(cs []Check) map[string]Check {
	m := map[string]Check{}
	for _, c := range cs {
		m[c.Name] = c
	}
	return m
}

const allHooks = `{"hooks":{
 "SessionStart":[{"hooks":[{"type":"command","command":"total-recall ingest --hook --detach"}]}],
 "Stop":[{"hooks":[{"type":"command","command":"total-recall ingest --hook --detach"}]}],
 "SubagentStop":[{"hooks":[{"type":"command","command":"total-recall ingest --hook --detach"}]}],
 "SessionEnd":[{"hooks":[{"type":"command","command":"total-recall ingest --hook --detach"},{"type":"command","command":"total-recall backup --if-older 24h --detach"}]}]}}`

func TestSetupChecksNothingInstalled(t *testing.T) {
	env, home := setupEnv(t)
	env.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	cs := SetupChecks(env, time.Now())
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name
	}
	if strings.Join(names, ",") != "config,hooks,mcp,binary,backups,transcripts,archive" {
		t.Fatalf("checks = %v", names)
	}
	m := byName(cs)
	if !m["config"].OK || !strings.Contains(m["config"].Text, "loaded") {
		t.Errorf("config = %+v", m["config"])
	}
	for _, n := range []string{"hooks", "mcp", "binary", "backups", "transcripts", "archive"} {
		if m[n].OK {
			t.Errorf("%s is ok on an empty home: %+v", n, m[n])
		}
	}
	if !strings.Contains(m["hooks"].Text, "~/.claude/settings.json not found") {
		t.Errorf("hooks = %q", m["hooks"].Text)
	}
	if !m["mcp"].MCP || m["backups"].Fix != "create the directory" || m["backups"].FixFn == nil {
		t.Errorf("mcp/backups = %+v / %+v", m["mcp"], m["backups"])
	}
	_ = home
}

func TestSetupChecksInstalled(t *testing.T) {
	env, home := setupEnv(t)
	write(t, filepath.Join(home, ".claude", "settings.json"), allHooks)
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"total-recall":{"command":"total-recall","args":["mcp"]}}}`)
	write(t, filepath.Join(home, "bin", "total-recall"), "x")
	write(t, filepath.Join(home, ".claude", "projects", "x"), "")
	write(t, filepath.Join(home, "archive.db"), "")
	if err := os.MkdirAll(filepath.Join(home, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := byName(SetupChecks(env, time.Now()))
	for n, c := range m {
		if !c.OK {
			t.Errorf("%s not ok: %+v", n, c)
		}
	}
	if m["hooks"].Text != "~/.claude/settings.json: SessionStart, Stop, SubagentStop, SessionEnd" {
		t.Errorf("hooks = %q", m["hooks"].Text)
	}
	if !strings.Contains(m["binary"].Text, "~/bin/total-recall on PATH, this version (1.2.3)") {
		t.Errorf("binary = %q", m["binary"].Text)
	}
	// A hook set without the backup on SessionEnd lacks that event.
	write(t, filepath.Join(home, ".claude", "settings.json"), strings.Replace(allHooks, `,{"type":"command","command":"total-recall backup --if-older 24h --detach"}`, "", 1))
	if c := byName(SetupChecks(env, time.Now()))["hooks"]; c.OK || !strings.Contains(c.Text, "lacks SessionEnd") {
		t.Errorf("hooks without backup = %+v", c)
	}
	// Another version on PATH is a warning on an ok line.
	env.Executable = func() (string, error) { return filepath.Join(home, "elsewhere"), nil }
	env.VersionOf = func(string) (string, error) { return "1.0.0", nil }
	if c := byName(SetupChecks(env, time.Now()))["binary"]; !c.OK || !c.Warn || !strings.Contains(c.Text, "is 1.0.0, this is 1.2.3") {
		t.Errorf("binary = %+v", c)
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		d    time.Duration
		want string
	}{{30 * time.Second, "30s ago"}, {5 * time.Minute, "5m ago"}, {3*time.Hour + 5*time.Minute, "3h05m ago"}, {50 * time.Hour, "2d02h ago"}, {-time.Hour, "in the future"}} {
		if got := Age(now, now.Add(-tt.d)); got != tt.want {
			t.Errorf("Age(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
