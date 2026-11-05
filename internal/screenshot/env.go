package screenshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/configview"
)

// Env is what the shots render against: the demo archive and a synthetic
// home directory with the files the config view looks at. Every path the
// frames show lies below Home, so it appears as "~/…".
type Env struct {
	// Store is the demo archive, read-only.
	Store *store.Store
	// Home is the synthetic home directory.
	Home string
	// Config is the configuration read from Home/.config/total-recall.
	Config *config.Config
	// View are the options of the config view: the installation the setup
	// page finds.
	View configview.Options
	// Outdated is the same home with an empty archive of the previous
	// schema version in place of the demo archive; the shot of a pending
	// migration renders against it. nil in Outdated itself.
	Outdated *Env
}

// Version is what the setup page shows as the version of the binary.
const Version = "1.2.0"

// backupAt is the time of the backup copy in the synthetic home: the night
// before the demo's clock.
var backupAt = time.Date(2026, 11, 2, 3, 0, 0, 0, demo.Location)

// NewEnv opens the demo archive at dbPath read-only and lays out a home
// directory below dir: a config file written from the template with the
// archive linked in at the default place and the project definitions of
// the demo, a settings file of Claude Code with the hooks, a backup copy
// of the night before, a projects directory and the binary on PATH. A
// second home with an outdated archive is Outdated. The caller removes dir.
func NewEnv(dbPath, dir string) (*Env, error) {
	e, err := newHome(filepath.Join(dir, "home"), dbPath, 0)
	if err != nil {
		return nil, err
	}
	if e.Outdated, err = newHome(filepath.Join(dir, "home-outdated"), "", store.LatestSchemaVersion()-1); err != nil {
		_ = e.Store.Close()
		return nil, err
	}
	return e, nil
}

// projects are the [[project]] definitions of the synthetic config: the
// repositories of the demo archive with names, colours and patterns.
const projects = `
[[project]]
name = "kompass"
description = "Wanderkarten, alle Repositories"
color = "blue"
match = ["https://git.invalid/mara/kompass*", "~/src/kompass*"]

[[project]]
name = "wetterwarte"
description = "Messstation und Auswertung"
color = "orange"
match = "https://git.invalid/mara/wetterwarte"

[[project]]
name = "fahrtenbuch"
color = "green"
match = "https://git.invalid/mara/fahrtenbuch"

[[project]]
name = "notizen"
description = "Markdown-Notizen ohne Repository"
match = "~/notizen"
`

// newHome lays out one home directory. With dbPath the demo archive is
// linked in as the archive; without, an empty archive is created with the
// schema version v.
func newHome(home, dbPath string, v int) (*Env, error) {
	paths := map[string]string{
		"config":   filepath.Join(home, ".config", "total-recall", "config.toml"),
		"db":       filepath.Join(home, ".local", "share", "total-recall", "archive.db"),
		"settings": filepath.Join(home, ".claude", "settings.json"),
		"backup":   filepath.Join(home, "Documents", "total-recall", store.BackupFileName(backupAt)),
		"binary":   filepath.Join(home, ".local", "bin", "total-recall"),
	}
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, fmt.Errorf("screenshot: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if dbPath != "" {
		abs, err := filepath.Abs(dbPath)
		if err != nil {
			return nil, fmt.Errorf("screenshot: %w", err)
		}
		if err := os.Symlink(abs, paths["db"]); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("screenshot: %w", err)
		}
	} else if err := store.CreateAtVersion(paths["db"], v); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := os.WriteFile(paths["config"], []byte(config.Template+projects), 0o644); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := os.WriteFile(paths["settings"], settingsJSON(), 0o644); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := os.WriteFile(paths["backup"], []byte("not a real backup"), 0o644); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := os.Chtimes(paths["backup"], backupAt, backupAt); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := os.WriteFile(paths["binary"], []byte("#!/bin/sh\n"), 0o755); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	cfg, err := config.Load(config.Options{GOOS: "linux", Home: home, Getenv: func(string) string { return "" }})
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	st, err := store.OpenReadOnly(paths["db"])
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	return &Env{
		Store: st, Home: home, Config: cfg,
		View: configview.Options{
			Version:      Version,
			SettingsFile: paths["settings"],
			Getenv:       func(string) string { return "" },
			LookPath:     func(string) (string, error) { return paths["binary"], nil },
			Executable:   func() (string, error) { return paths["binary"], nil },
			VersionOf:    func(string) (string, error) { return Version, nil },
		},
	}, nil
}

// Close closes the archives.
func (e *Env) Close() error {
	err := e.Store.Close()
	if e.Outdated != nil {
		return errors.Join(err, e.Outdated.Store.Close())
	}
	return err
}

// settingsJSON is a Claude Code settings file with the hook block.
func settingsJSON() []byte {
	b, err := json.MarshalIndent(map[string]any{"theme": "dark", "hooks": ops.ClaudeHooks("total-recall")["hooks"]}, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}
