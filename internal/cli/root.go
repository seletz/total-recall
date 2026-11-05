// Package cli implements the total-recall command line (cobra). The command
// tree is built by NewRoot so tests can run it in-process with their own
// stdin/stdout, environment and detach hook.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/version"
)

// App holds the process environment of a command run. Zero fields use the
// real process environment.
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Args are the command-line arguments without the program name; they are
	// re-used (minus --detach) when a command re-executes itself detached.
	Args []string
	// Getenv looks up environment variables (config and CLAUDE_CONFIG_DIR).
	Getenv func(string) string
	// Home overrides the home directory.
	Home string
	// Now is the clock; default time.Now.
	Now func() time.Time
	// StartDetached starts this binary with args in a new session, detached
	// from the caller, with stdout/stderr appended to logPath. Default:
	// startDetached (os/exec with Setsid).
	StartDetached func(args []string, logPath string) error
	// TerminalWidth returns the width of the terminal Stdout is attached
	// to, or 0 when it is not a terminal (table cells are then never cut).
	// Default: terminalWidth(Stdout).
	TerminalWidth func() int
	// Context is the context of the run; a long-running command (serve)
	// stops when it is done. Default: context.Background.
	Context context.Context
}

func (a *App) defaults() {
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Getenv == nil {
		a.Getenv = os.Getenv
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	if a.StartDetached == nil {
		a.StartDetached = startDetached
	}
	if a.TerminalWidth == nil {
		a.TerminalWidth = func() int { return terminalWidth(a.Stdout) }
	}
	if a.Context == nil {
		a.Context = context.Background()
	}
}

// globals are the persistent flags.
type globals struct {
	config    string
	db        string
	tz        string
	backupDir string
	// detachedChild is set (hidden flag) in a process started by --detach:
	// normal output then goes to the log file with timestamps.
	detachedChild bool
}

// Main runs the CLI with args and returns the process exit code. An error
// goes to stderr; when a JSON format was requested it is also written to
// stdout as an ErrorJSON object, so a consumer of stdout never sees nothing.
func Main(app *App) int {
	app.defaults()
	root, e := newRoot(app)
	root.SetArgs(app.Args)
	if err := root.ExecuteContext(app.Context); err != nil {
		var ec exitCode
		if errors.As(err, &ec) {
			return int(ec)
		}
		if errors.Is(err, errSilent) {
			return 1
		}
		_, _ = fmt.Fprintln(app.Stderr, "total-recall:", err)
		if e.jsonErrors || jsonRequested(app.Args) {
			writeJSONError(app.Stdout, err)
		}
		return 1
	}
	return 0
}

// NewRoot builds the command tree.
func NewRoot(app *App) *cobra.Command {
	root, _ := newRoot(app)
	return root
}

// newRoot builds the command tree and returns the run's environment.
func newRoot(app *App) (*cobra.Command, *env) {
	app.defaults()
	g := &globals{}
	root := &cobra.Command{
		Use:           "total-recall",
		Short:         "Archive and query coding-agent sessions",
		Long:          "total-recall archives coding-agent sessions (Claude Code) into a local SQLite database and makes them searchable.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// --version (and -v) prints the same line as the version subcommand.
		Version: version.Version,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetIn(app.Stdin)
	root.SetOut(app.Stdout)
	root.SetErr(app.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage(err) })
	pf := root.PersistentFlags()
	pf.StringVar(&g.config, "config", "", "config file (default ~/.config/total-recall/config.toml, env TOTAL_RECALL_CONFIG)")
	pf.StringVar(&g.db, "db", "", "database path (env TOTAL_RECALL_DB)")
	pf.StringVar(&g.tz, "tz", "", "time zone for display and day boundaries (default from config, Europe/Berlin)")
	pf.StringVar(&g.backupDir, "backup-dir", "", "backup directory (env TOTAL_RECALL_BACKUP_DIR)")
	pf.BoolVar(&g.detachedChild, "detached-child", false, "internal: running as detached child")
	_ = pf.MarkHidden("detached-child")

	e := &env{app: app, g: g}
	root.AddCommand(
		e.ingestCmd(),
		e.reindexCmd(),
		e.statusCmd(),
		e.sessionsCmd(),
		e.showCmd(),
		e.searchCmd(),
		e.statsCmd(),
		e.chartCmd(),
		e.sqlCmd(),
		e.backupCmd(),
		e.restoreCmd(),
		e.maintenanceCmd(),
		e.migrateCmd(),
		e.hooksCmd(),
		e.doctorCmd(),
		e.configCmd(),
		e.llmCmd(),
		e.tuiCmd(),
		e.serveCmd(),
		versionCmd(),
	)
	return root, e
}

// env is shared by all commands of one run.
type env struct {
	app *App
	g   *globals
	// jsonErrors is set by a read command that resolved a JSON format:
	// Main then reports an error as JSON on stdout too.
	jsonErrors bool
	// schemaOutdated is set by openRead when the archive has an older
	// schema than this binary: query errors then carry the migrate hint.
	schemaOutdated bool
}

// loadConfig loads the effective configuration with the global flags. Its
// errors are of kind config.
func (e *env) loadConfig() (*config.Config, error) {
	cfg, err := config.Load(config.Options{
		ConfigFile: e.g.config,
		Flags:      config.Flags{DB: e.g.db, BackupDir: e.g.backupDir, Timezone: e.g.tz},
		Home:       e.app.Home,
		Getenv:     e.app.Getenv,
	})
	return cfg, errKind(ErrKindConfig, err)
}

// out returns where normal output goes: stdout, or the log in a detached child.
func (e *env) out(cfg *config.Config) io.Writer {
	if e.g.detachedChild && cfg != nil {
		return logfile.New(cfg.LogPath(), e.app.Now).Writer("INFO")
	}
	return e.app.Stdout
}

// childArgs returns the arguments for a detached re-execution: the original
// arguments without --detach, plus the hidden --detached-child flag.
func childArgs(args []string, extra ...string) []string {
	out := make([]string, 0, len(args)+1+len(extra))
	for _, a := range args {
		if a == "--detach" || a == "--detach=true" || a == "--detach=false" {
			continue
		}
		out = append(out, a)
	}
	out = append(out, "--detached-child")
	return append(out, extra...)
}

// exitCode is returned by a command that has reported its result and wants
// a specific process exit code (e.g. doctor: 2 = findings).
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("exit code %d", int(c)) }

// errSilent signals a failure that has already been reported.
var errSilent = errors.New("failed")

// versionCmd is the version subcommand; `--version` on the root prints the
// same line.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version (same as --version)",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), version.Version)
		},
	}
}
