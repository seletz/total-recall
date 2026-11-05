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
}

// Version is what the setup page shows as the version of the binary.
const Version = "1.2.0"

// backupAt is the time of the backup copy in the synthetic home: the night
// before the demo's clock.
var backupAt = time.Date(2026, 11, 2, 3, 0, 0, 0, demo.Location)

// NewEnv opens the demo archive at dbPath read-only and lays out a home
// directory below dir: a config file written from the template with the
// archive linked in at the default place, a settings file of Claude Code
// with the hooks, a backup copy of the night before, a projects directory
// and the binary on PATH. The caller removes dir.
func NewEnv(dbPath, dir string) (*Env, error) {
	home := filepath.Join(dir, "home")
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
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := os.Symlink(abs, paths["db"]); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := config.WriteTemplate(paths["config"]); err != nil && !errors.Is(err, os.ErrExist) {
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
	st, err := store.OpenReadOnly(dbPath)
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

// Close closes the archive.
func (e *Env) Close() error { return e.Store.Close() }

// settingsJSON is a Claude Code settings file with the hook block.
func settingsJSON() []byte {
	b, err := json.MarshalIndent(map[string]any{"theme": "dark", "hooks": ops.ClaudeHooks("total-recall")["hooks"]}, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}
