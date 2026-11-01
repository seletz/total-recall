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
		Short: "Write a consistent copy of the archive (VACUUM INTO) and prune old copies",
		Long: `Write dir/archive-YYYY-MM-DD.db (date in the configured time zone) with
VACUUM INTO, then keep the newest backup.keep_daily copies plus the newest copy
of each month (backup.keep_monthly months, 0 = all).`,
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

// ConfigJSON is the --json output of config show.
type ConfigJSON struct {
	File       string        `json:"file"`
	FileOrigin string        `json:"file_origin"`
	Loaded     bool          `json:"loaded"`
	Values     []ConfigValue `json:"values"`
}

// ConfigValue is one effective value.
type ConfigValue struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Origin string `json:"origin"`
}

func (e *env) configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Show or initialize the configuration"}
	var asJSON bool
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the effective configuration with the origin of each value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			out := ConfigJSON{File: cfg.Path, FileOrigin: string(cfg.PathOrigin), Loaded: cfg.Loaded}
			for _, en := range cfg.Entries() {
				out.Values = append(out.Values, ConfigValue{Key: en.Key, Value: en.Value, Origin: string(en.Origin)})
			}
			out.Values = append(out.Values, ConfigValue{Key: "log (effective)", Value: cfg.LogPath(), Origin: string(cfg.Origin("log"))})
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), out)
			}
			state := "not found, defaults apply"
			if cfg.Loaded {
				state = "loaded"
			}
			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "config file: %s (%s, %s)\n\n", cfg.Path, cfg.PathOrigin, state)
			rows := make([][]string, 0, len(out.Values))
			for _, v := range out.Values {
				rows = append(rows, []string{v.Key, v.Value, v.Origin})
			}
			return table(w, []string{"KEY", "VALUE", "ORIGIN"}, rows)
		},
	}
	show.Flags().BoolVar(&asJSON, "json", false, "output JSON")
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
