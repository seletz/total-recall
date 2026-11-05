// Package ops is the wiring the write-side commands of total-recall share:
// the lock directory, opening the archive for writing under the
// maintenance lock, the ingest dependencies and the harness adapters built
// from the configuration, the hook block of Claude Code. The CLI commands
// (ingest, reindex, backup, maintenance, doctor, hooks) and the config tab
// of the TUI call the same functions, so an action started from the TUI
// behaves exactly like its command.
package ops

import (
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/redact"
	"github.com/seletz/total-recall/internal/store"
)

// RecommendedPageSize is the page size maintenance uses by default and
// status recommends: rows of 1 to 4 KiB waste far less overflow space in
// 16 KiB pages than in SQLite's default 4 KiB pages.
const RecommendedPageSize = 16384

// LockDir is the directory of the ingest and maintenance lock files,
// "locks" next to the database.
func LockDir(cfg *config.Config) string { return filepath.Join(filepath.Dir(cfg.DB), "locks") }

// OpenWrite opens the archive read-write (migrating it if needed) and holds
// the shared maintenance lock until Close: while `maintenance` rebuilds the
// archive, it fails with store.ErrMaintenance.
func OpenWrite(cfg *config.Config, log *logfile.Logger) (*store.Store, error) {
	return store.Open(cfg.DB, store.WithLocation(cfg.Location()),
		store.WithLockDir(LockDir(cfg)),
		store.WithLogf(func(f string, a ...any) { log.Infof(f, a...) }))
}

// Deps builds the ingest dependencies from the configuration.
func Deps(cfg *config.Config, st *store.Store, log *logfile.Logger) (ingest.Deps, error) {
	d := ingest.Deps{
		Store:       st,
		StoreRaw:    cfg.Ingest.StoreRaw,
		RawMaxBytes: cfg.Ingest.RawMaxBytes,
		ResolveGit:  cfg.Git.Resolve,
		LockDir:     LockDir(cfg),
		Logf:        func(f string, a ...any) { log.Infof(f, a...) },
	}
	if cfg.Redact.Enabled {
		r, err := redact.New(redact.Options{ExtraPatterns: cfg.Redact.ExtraPatterns, Allow: cfg.Redact.Allow})
		if err != nil {
			return d, err
		}
		d.Redactor = r
	}
	d.Adapters = Adapters(cfg)
	return d, nil
}

// Adapters returns the enabled harness adapters configured from cfg.
func Adapters(cfg *config.Config) []harness.Adapter {
	var out []harness.Adapter
	if c := cfg.Harness.Claude; c.Enabled {
		out = append(out, claude.New(c.ProjectsDir,
			claude.WithToolResultMaxBytes(cfg.Ingest.ToolResultMaxBytes),
			claude.WithPeerPrefixes(c.Actors.PeerPrefixes...),
			claude.WithPeerPasteHeaders(c.Actors.PeerPasteHeaders...),
			claude.WithAutomationPrefixes(c.Actors.AutomationPrefixes...),
			claude.WithSkipRecordTypes(c.SkipRecordTypes...),
		))
	}
	return out
}

// MaintainOptions are the options of store.Maintain for cfg: the
// recommended page size unless pageSize is given, the lock directory, the
// backup settings and now in the configured time zone.
func MaintainOptions(cfg *config.Config, now time.Time, pageSize int, dryRun bool, wait time.Duration) store.MaintainOptions {
	if pageSize == 0 {
		pageSize = RecommendedPageSize
	}
	return store.MaintainOptions{
		PageSize:    pageSize,
		DryRun:      dryRun,
		LockDir:     LockDir(cfg),
		Wait:        wait,
		BackupDir:   cfg.Backup.Dir,
		Now:         now.In(cfg.Location()),
		KeepDaily:   cfg.Backup.KeepDaily,
		KeepMonthly: cfg.Backup.KeepMonthly,
	}
}

// HookCommand is one hook handler in Claude Code's settings.json.
type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookMatcher is one entry of an event's hook list.
type HookMatcher struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

// HookEvents are the Claude Code events total-recall hooks into, in the
// order of the docs.
var HookEvents = []string{"SessionStart", "Stop", "SubagentStop", "SessionEnd"}

// ClaudeHooks returns the settings.json "hooks" block for binary: ingest on
// every event, a daily backup on SessionEnd.
func ClaudeHooks(binary string) map[string]any {
	ingest := HookCommand{Type: "command", Command: binary + " ingest --hook --detach"}
	backup := HookCommand{Type: "command", Command: binary + " backup --if-older 24h --detach"}
	one := func(h ...HookCommand) []HookMatcher { return []HookMatcher{{Hooks: h}} }
	return map[string]any{"hooks": map[string][]HookMatcher{
		"SessionStart": one(ingest),
		"Stop":         one(ingest),
		"SubagentStop": one(ingest),
		"SessionEnd":   one(ingest, backup),
	}}
}
