// Package ops is the wiring the write-side commands of total-recall share:
// the lock directory, opening the archive for writing under the
// maintenance lock, the ingest dependencies and the harness adapters built
// from the configuration, the hook block of Claude Code. The CLI commands
// (ingest, reindex, backup, maintenance, doctor, hooks) and the config tab
// of the TUI call the same functions, so an action started from the TUI
// behaves exactly like its command.
package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/dispatch"
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

// OpenWrite opens the archive read-write and holds the shared maintenance
// lock until Close: while `maintenance` or `migrate` holds the archive, it
// fails with store.ErrMaintenance. It never migrates: an archive with an
// older schema is store.ErrSchemaOutdated unless extra has
// store.AllowOutdated (backup).
func OpenWrite(cfg *config.Config, log *logfile.Logger, extra ...store.Option) (*store.Store, error) {
	opts := append([]store.Option{store.WithLocation(cfg.Location()),
		store.WithLockDir(LockDir(cfg)),
		store.WithLogf(func(f string, a ...any) { log.Infof(f, a...) })}, extra...)
	return store.Open(cfg.DB, opts...)
}

// Deps builds the ingest dependencies from the configuration.
func Deps(cfg *config.Config, st *store.Store, log *logfile.Logger) (ingest.Deps, error) {
	d := ingest.Deps{
		Store:       st,
		StoreRaw:    cfg.Ingest.StoreRaw,
		RawMaxBytes: cfg.Ingest.RawMaxBytes,
		StoreBlobs:  cfg.Ingest.StoreBlobs,
		StoreMemory: cfg.Ingest.StoreMemory,
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
	d.Adapters = adapters(cfg, d.Logf)
	d.Dispatch = dispatch.Config{Window: cfg.Harness.Claude.Actors.PromptWindow.Duration, Commands: cfg.Harness.Claude.Actors.PromptCommands, SpawnCommands: cfg.Harness.Claude.Actors.SpawnCommands}
	if d.Dispatch.Window == 0 {
		d.Dispatch.Window = -1 // 0 in the configuration turns the detection off
	}
	return d, nil
}

// Adapters returns the enabled harness adapters configured from cfg.
func Adapters(cfg *config.Config) []harness.Adapter { return adapters(cfg, nil) }

// adapters builds the adapters; logf receives their diagnostics (nil:
// discarded).
func adapters(cfg *config.Config, logf func(string, ...any)) []harness.Adapter {
	var out []harness.Adapter
	if c := cfg.Harness.Claude; c.Enabled {
		opts := []claude.Option{
			claude.WithToolResultMaxBytes(cfg.Ingest.ToolResultMaxBytes),
			claude.WithPeerPrefixes(c.Actors.PeerPrefixes...),
			claude.WithPeerPasteHeaders(c.Actors.PeerPasteHeaders...),
			claude.WithAutomationPrefixes(c.Actors.AutomationPrefixes...),
			claude.WithSkipRecordTypes(c.SkipRecordTypes...),
			claude.WithLogf(logf),
		}
		if cfg.Ingest.StoreFileHistory && cfg.Ingest.StoreRaw {
			// The backups live next to the transcripts: projects_dir is
			// $CLAUDE_CONFIG_DIR/projects, the copies are in
			// $CLAUDE_CONFIG_DIR/file-history.
			opts = append(opts, claude.WithFileHistoryDir(filepath.Join(filepath.Dir(c.ProjectsDir), "file-history")))
		}
		out = append(out, claude.New(c.ProjectsDir, opts...))
	}
	return out
}

// PlanLines renders a migration plan as the lines `migrate` prints after
// the database line: the schema versions and every pending step, the
// pre-migration copy and the free space needed.
func PlanLines(plan store.MigratePlan) []string {
	if !plan.Pending() {
		return []string{fmt.Sprintf("schema    v%d is current, nothing to migrate", plan.From)}
	}
	noun := "migrations"
	if len(plan.Steps) == 1 {
		noun = "migration"
	}
	out := []string{fmt.Sprintf("schema    v%d -> v%d, %d pending %s:", plan.From, plan.To, len(plan.Steps), noun)}
	for _, s := range plan.Steps {
		out = append(out, fmt.Sprintf("  v%-4d %s", s.Version, s.Summary))
	}
	out = append(out, "backup    "+plan.Backup,
		fmt.Sprintf("space     about %.1f MiB free needed next to the database (copy and migration)", float64(plan.NeedBytes)/(1<<20)))
	return out
}

// Migrate brings the archive of cfg to the schema of this binary as the
// `migrate` command does: with the maintenance lock, waiting up to wait
// for running hooks, one line per step to logf and the progress of a long
// step to progress (both optional).
func Migrate(ctx context.Context, cfg *config.Config, wait time.Duration, logf func(string, ...any), progress func(store.Progress)) (store.MigrateResult, error) {
	return store.Migrate(ctx, cfg.DB, store.MigrateOptions{LockDir: LockDir(cfg), Wait: wait, Logf: logf, Progress: progress})
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

// MCPServerName is the name of the total-recall entry in the MCP
// configuration of a harness.
const MCPServerName = "total-recall"

// MCPServer is the entry of the "mcpServers" object of Claude Code.
type MCPServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// ClaudeMCPServer returns the entry for the "mcpServers" object of Claude
// Code (stdio, started by the harness as a child process).
func ClaudeMCPServer(binary string) MCPServer {
	return MCPServer{Type: "stdio", Command: binary, Args: []string{"mcp"}}
}

// ClaudeMCPConfig returns the {"mcpServers": ...} block for binary, as it
// goes into a project's .mcp.json or the mcpServers key of ~/.claude.json.
func ClaudeMCPConfig(binary string) map[string]any {
	return map[string]any{"mcpServers": map[string]any{MCPServerName: ClaudeMCPServer(binary)}}
}

// ClaudeMCPAddCommand returns the `claude mcp add` command line that
// registers the server for the user.
func ClaudeMCPAddCommand(binary string) string {
	return "claude mcp add --scope user " + MCPServerName + " -- " + binary + " mcp"
}
