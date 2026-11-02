package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/skill"
	"github.com/seletz/total-recall/internal/store"
)

// ---- backup ----

func (e *env) backupCmd() *cobra.Command {
	var ifOlder string
	var detach bool
	c := &cobra.Command{
		Use:   "backup",
		Short: "Write a compressed, consistent copy of the archive and prune old copies",
		Long: `Write dir/archive-YYYY-MM-DD.db.gz (date in the configured time zone): a
VACUUM INTO copy, gzip-compressed. Then keep the newest backup.keep_daily copies
plus the newest copy of each month (backup.keep_monthly months, 0 = all).
Uncompressed copies (archive-YYYY-MM-DD.db) from older versions count alike.
Restore a copy with "total-recall restore".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			log := newLogger(cfg.LogPath(), e.app.Now)
			if detach {
				if err := e.app.StartDetached(childArgs(e.app.Args), cfg.LogPath()); err != nil {
					log.Errorf("backup: detach: %v", err)
				}
				return nil
			}
			err = e.runBackup(cmd, cfg, ifOlder, log)
			if err != nil && e.g.detachedChild {
				log.Errorf("backup: %v", err)
				return errSilent
			}
			return err
		},
	}
	c.Flags().StringVar(&ifOlder, "if-older", "", "only back up if the newest copy is older than this duration, e.g. 24h")
	c.Flags().BoolVar(&detach, "detach", false, "run in a detached background process and return immediately")
	return c
}

func (e *env) runBackup(cmd *cobra.Command, cfg *config.Config, ifOlder string, log *logger) error {
	out := e.out(cfg)
	now := e.app.Now()
	if ifOlder != "" {
		d, err := time.ParseDuration(ifOlder)
		if err != nil {
			return fmt.Errorf("invalid --if-older %q: %w", ifOlder, err)
		}
		p, t, err := store.LastBackup(cfg.Backup.Dir)
		if err != nil {
			return err
		}
		if p != "" && now.Sub(t) < d {
			_, _ = fmt.Fprintf(out, "backup skipped: %s is %s old (< %s)\n", p, duration(now.Sub(t)), d)
			return nil
		}
	}
	if _, err := os.Stat(cfg.DB); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no archive at %s", cfg.DB)
	}
	st, err := openWrite(cfg, log)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	p, err := st.Backup(cmd.Context(), cfg.Backup.Dir, now.In(cfg.Location()), cfg.Backup.KeepDaily, cfg.Backup.KeepMonthly)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "backup written: %s\n", p)
	return nil
}

// ---- restore ----

func (e *env) restoreCmd() *cobra.Command {
	var to string
	var force bool
	c := &cobra.Command{
		Use:   "restore BACKUP_FILE",
		Short: "Restore a backup copy (.db.gz or .db) into a database file",
		Long: `Decompress BACKUP_FILE into a database file: --to PATH, default the
configured database. An existing target is never replaced unless --force is
given; then its -wal/-shm files are removed too, so stop running ingests first.
The restored data is checked before the target is touched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			dst := to
			if dst == "" {
				dst = cfg.DB
			}
			if err := store.Restore(cmd.Context(), args[0], dst, force); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "restored %s to %s\n", args[0], dst)
			return nil
		},
	}
	c.Flags().StringVar(&to, "to", "", "target database path (default: the configured database)")
	c.Flags().BoolVar(&force, "force", false, "replace an existing target database")
	return c
}

// ---- hooks ----

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

// ClaudeHooks returns the settings.json "hooks" block for binary.
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

func (e *env) hooksCmd() *cobra.Command {
	c := &cobra.Command{Use: "hooks", Short: "Hook configuration for harnesses"}
	var h, binary string
	p := &cobra.Command{
		Use:   "print",
		Short: "Print the hook configuration (JSON) for a harness",
		Long: `Print the hooks block for the harness settings. For Claude Code, merge it
into ~/.claude/settings.json (do not replace existing hooks).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if h != "claude" {
				return fmt.Errorf("unknown harness %q (supported: claude)", h)
			}
			return writeJSON(cmd.OutOrStdout(), ClaudeHooks(binary))
		},
	}
	p.Flags().StringVar(&h, "harness", "claude", "harness: claude")
	p.Flags().StringVar(&binary, "binary", "total-recall", "command used in the hooks, e.g. an absolute path")
	c.AddCommand(p)
	return c
}

// ---- config ----

// ConfigJSON is the JSON output of config show. Every key is always
// present; Problems is [] without problems.
type ConfigJSON struct {
	File       string        `json:"file"`
	FileOrigin string        `json:"file_origin"`
	Loaded     bool          `json:"loaded"`
	Values     []ConfigValue `json:"values"`
	// Problems are the errors of the tables that are checked when used
	// ([work], [tui], [keys]); the command exits non-zero when there are any.
	Problems []string `json:"problems"`
}

// ConfigValue is one effective value; Value is null when empty.
type ConfigValue struct {
	Key    string     `json:"key"`
	Value  NullString `json:"value"`
	Origin string     `json:"origin"`
}

func (e *env) configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Show or initialize the configuration"}
	var of outputFlags
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the effective configuration with the origin of each value",
		Args:  usageArgs(cobra.NoArgs),
	}
	show.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		out := ConfigJSON{File: cfg.Path, FileOrigin: string(cfg.PathOrigin), Loaded: cfg.Loaded, Values: []ConfigValue{}, Problems: []string{}}
		for _, en := range cfg.Entries() {
			out.Values = append(out.Values, ConfigValue{Key: en.Key, Value: NullString(en.Value), Origin: string(en.Origin)})
		}
		out.Values = append(out.Values, ConfigValue{Key: "log (effective)", Value: NullString(cfg.LogPath()), Origin: string(cfg.Origin("log"))})
		problems := cfg.Problems()
		for _, p := range problems {
			out.Problems = append(out.Problems, p.Error())
		}
		if o.format.IsJSON() {
			if err := o.object(out); err != nil {
				return err
			}
			if len(problems) > 0 {
				return errSilent
			}
			return nil
		}
		state := "not found, defaults apply"
		if cfg.Loaded {
			state = "loaded"
		}
		w := o.w
		_, _ = fmt.Fprintf(w, "config file: %s (%s, %s)\n\n", cfg.Path, cfg.PathOrigin, state)
		rows := make([][]string, 0, len(out.Values))
		for _, v := range out.Values {
			rows = append(rows, []string{v.Key, string(v.Value), v.Origin})
		}
		if err := o.table([]string{"KEY", "VALUE", "ORIGIN"}, rows); err != nil {
			return err
		}
		if len(problems) == 0 {
			return nil
		}
		_, _ = fmt.Fprintln(w, "\nproblems (checked when a command uses the table; hooks and ingest are not affected):")
		for _, p := range problems {
			_, _ = fmt.Fprintf(w, "  %s\n", p)
		}
		return errSilent
	})
	of.register(show)
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write a commented config template (never overwrites)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := e.loadConfig()
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			path := e.g.config
			if cfg != nil {
				path = cfg.Path
			}
			if err := config.WriteTemplate(path); err != nil {
				if errors.Is(err, fs.ErrExist) {
					return fmt.Errorf("%s exists, not overwriting", path)
				}
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	}
	c.AddCommand(show, initCmd)
	return c
}

// ---- llm ----

func (e *env) llmCmd() *cobra.Command {
	var skillFlag bool
	c := &cobra.Command{
		Use:   "llm",
		Short: "Output for coding agents (--skill: the embedded SKILL.md)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !skillFlag {
				return fmt.Errorf("llm: use --skill")
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), skill.Text())
			return err
		},
	}
	c.Flags().BoolVar(&skillFlag, "skill", false, "print the agent skill (SKILL.md)")
	return c
}
