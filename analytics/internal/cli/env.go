package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/analytics/internal/star"
	"github.com/seletz/total-recall/internal/config"
)

// ErrKindConfig: the configuration could not be loaded.
const ErrKindConfig = "config"

// config loads total-recall's configuration, with --archive and --tz as
// overrides. The analytics tool reads the same file as total-recall: archive
// path, time zone, [[project]] definitions and logins.
func (a *App) config() (*config.Config, error) {
	cfg, err := config.Load(config.Options{
		ConfigFile: a.configFile,
		Flags:      config.Flags{DB: a.archive, Timezone: a.tz},
		Home:       a.Home,
		Getenv:     a.Getenv,
		Hostname:   a.Hostname,
	})
	return cfg, errKind(ErrKindConfig, err)
}

// analyticsPath is --db, else analytics.duckdb next to the archive.
func (a *App) analyticsPath(cfg *config.Config) string {
	if a.dbPath != "" {
		return a.dbPath
	}
	return filepath.Join(filepath.Dir(cfg.DB), "analytics.duckdb")
}

// openAnalytics opens the analytics database of the configuration.
func (a *App) openAnalytics(ctx context.Context, cfg *config.Config, readOnly bool) (*duck.DB, string, error) {
	path := a.analyticsPath(cfg)
	if readOnly {
		if _, err := os.Stat(path); err != nil {
			return nil, path, errKind(ErrKindDB, fmt.Errorf("analytics database %s: %w (run build first)", path, star.ErrNotBuilt))
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		// A machine that only analyses the pool has no archive, and so no
		// folder for the default path next to it.
		return nil, path, errKind(ErrKindDB, err)
	}
	db, err := a.open(ctx, duck.Options{Path: path, ReadOnly: readOnly})
	if err != nil || !readOnly {
		return db, path, err
	}
	// A build that failed leaves a database without views.
	if ok, err := star.Built(ctx, db); err != nil || !ok {
		_ = db.Close()
		if err == nil {
			err = fmt.Errorf("analytics database %s: %w (run build first)", path, star.ErrNotBuilt)
		}
		return nil, path, errKind(ErrKindDB, err)
	}
	return db, path, nil
}

// hostName is --host, else the short host name of this machine.
func (a *App) hostName(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	h, err := a.Hostname()
	if err != nil {
		return "", fmt.Errorf("host name: %w (set --host)", err)
	}
	h, _, _ = strings.Cut(h, ".")
	return h, nil
}

// location is the zone of the config (already overridden by --tz).
func location(cfg *config.Config) (*time.Location, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	return loc, errKind(ErrKindConfig, err)
}
