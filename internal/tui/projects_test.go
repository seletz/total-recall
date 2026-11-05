package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/tui/configview"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// loadConfig loads a configuration from a file with the given [[project]]
// definitions below a temporary home.
func loadConfig(t *testing.T, projects string) *config.Config {
	t.Helper()
	return loadConfigText(t, config.Template+projects)
}

// loadConfigText writes text as the configuration file below a temporary
// home and loads it.
func loadConfigText(t *testing.T, text string) *config.Config {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "total-recall")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{GOOS: "linux", Home: home, Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return filepath.Join(home, ".config")
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestProjectsChanged: a changed [[project]] list from the config view
// renames the projects of the stats and the activity view at once; a list
// with a problem keeps the definitions the app started with.
func TestProjectsChanged(t *testing.T) {
	h := newReloadHarness(t)
	h.now = time.Date(2026, 1, 10, 12, 0, 0, 0, testfixture.ActivityLocation)
	m := start(t, h.app(t, Options{Start: ViewStats}))
	requireContains(t, frame(m), "alpha", "beta")
	requireLacks(t, frame(m), "kompass")

	cfg := loadConfig(t, "\n[[project]]\nname = \"kompass\"\nmatch = \"https://example.test/o/alpha\"\n")
	m = tuitest.Send(t, m, configview.Changed{Config: cfg})
	requireContains(t, frame(m), "kompass", "beta")
	requireLacks(t, frame(m), "alpha")
	// The activity view too, with its legend by project.
	m = tuitest.Send(t, m, tuitest.Key("3"))
	m = tuitest.Send(t, m, tuitest.Key("p"))
	requireContains(t, frame(m), "kompass")
	requireLacks(t, frame(m), "alpha")

	// A broken list changes nothing.
	bad := loadConfig(t, "\n[[project]]\nname = \"iris\"\ncolor = \"mauve\"\nmatch = \"*\"\n")
	m = tuitest.Send(t, m, configview.Changed{Config: bad})
	m = tuitest.Send(t, m, tuitest.Key("2"))
	requireContains(t, frame(m), "kompass", "beta")
	requireLacks(t, frame(m), "iris")

	// Removing the definitions brings the derived names back.
	none := loadConfig(t, "")
	m = tuitest.Send(t, m, configview.Changed{Config: none})
	requireContains(t, frame(m), "alpha", "beta")
	requireLacks(t, frame(m), "kompass")
}
