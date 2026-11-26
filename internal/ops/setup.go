package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
)

// Check is one line of the setup checks: the TUI shows it on the setup
// page of Config, `doctor --setup` prints it.
type Check struct {
	Name string
	// OK is a tick, Warn a hint on an OK line; neither is a failure.
	OK, Warn bool
	Text     string
	// MCP marks the line about the MCP entry.
	MCP bool
	// Fix, when set, names what FixFn does; the caller asks before
	// running it. ReloadConfig says the configuration file changed by it.
	Fix          string
	FixFn        func() error
	ReloadConfig bool
}

// SetupEnv is what the setup checks look at. Zero fields take their
// defaults: the Claude Code files under $CLAUDE_CONFIG_DIR or ~/.claude,
// the working directory for .mcp.json, the real PATH and binary.
type SetupEnv struct {
	// Config is the effective configuration.
	Config *config.Config
	// Home is the home directory, shown as "~" in the texts.
	Home string
	// Version is the version of the running binary.
	Version string
	// Getenv looks up CLAUDE_CONFIG_DIR; nil means os.Getenv.
	Getenv func(string) string
	// SettingsFile is the Claude Code settings file whose hooks are
	// checked.
	SettingsFile string
	// ClaudeJSON is the file whose "mcpServers" key is read for the MCP
	// entry (only that key, never written).
	ClaudeJSON string
	// ProjectDir is the directory whose .mcp.json is read as well.
	ProjectDir string
	// LookPath finds the binary on PATH; nil means exec.LookPath.
	LookPath func(file string) (string, error)
	// Executable is the path of the running binary; nil means
	// os.Executable.
	Executable func() (string, error)
	// VersionOf asks the binary at path for its version; nil runs
	// "<path> --version".
	VersionOf func(path string) (string, error)
}

func (o *SetupEnv) defaults() {
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Executable == nil {
		o.Executable = os.Executable
	}
	if o.VersionOf == nil {
		o.VersionOf = VersionOf
	}
	if o.SettingsFile == "" {
		dir := o.Getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(o.Home, ".claude")
		}
		o.SettingsFile = filepath.Join(dir, "settings.json")
	}
	if o.ClaudeJSON == "" {
		if dir := o.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
			o.ClaudeJSON = filepath.Join(dir, ".claude.json")
		} else {
			o.ClaudeJSON = filepath.Join(o.Home, ".claude.json")
		}
	}
	if o.ProjectDir == "" {
		o.ProjectDir, _ = os.Getwd()
	}
}

// VersionOf runs "<path> --version" and returns the first line.
func VersionOf(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	v, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return v, nil
}

// tildeHome writes p with home as "~"; without a home it stays as it is.
func tildeHome(p, home string) string {
	if home == "" || p == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + rel
	}
	return p
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Age formats how long before now t was: "30s ago", "5m ago", "3h05m
// ago", "2d02h ago".
func Age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "in the future"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm ago", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh ago", int(d.Hours())/24, int(d.Hours())%24)
}

// SetupChecks examines the installation: the config file, the hooks in the
// Claude Code settings, the MCP entry, the binary on PATH, the backup
// directory, the transcripts and the archive. It only reads; the fixes it
// names are functions the caller may run after asking.
func SetupChecks(opts SetupEnv, now time.Time) []Check {
	opts.defaults()
	cfg := opts.Config
	if cfg == nil {
		return nil
	}
	tilde := func(p string) string { return tildeHome(p, opts.Home) }
	var out []Check

	// config file
	c := Check{Name: "config"}
	switch {
	case cfg.Loaded:
		c.OK, c.Text = true, tilde(cfg.Path)+" loaded"
		if n := len(cfg.Problems()); n > 0 {
			c.Warn, c.Text = true, c.Text+fmt.Sprintf(", %s (see status)", plural(n, "problem"))
		}
	default:
		c.Text = tilde(cfg.Path) + " missing (defaults apply)"
		c.Fix, c.ReloadConfig = "write template", true
		path := cfg.Path
		c.FixFn = func() error { return config.WriteTemplate(path) }
	}
	out = append(out, c)

	// hooks
	c = Check{Name: "hooks"}
	present, err := hooksIn(opts.SettingsFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.Text = tilde(opts.SettingsFile) + " not found; add the hook block (c copies it)"
	case err != nil:
		c.Text = tilde(opts.SettingsFile) + ": " + err.Error()
	default:
		var missing []string
		for _, ev := range HookEvents {
			if !present[ev] {
				missing = append(missing, ev)
			}
		}
		if len(missing) == 0 {
			c.OK, c.Text = true, tilde(opts.SettingsFile)+": "+strings.Join(HookEvents, ", ")
		} else {
			c.Text = tilde(opts.SettingsFile) + " lacks " + strings.Join(missing, ", ") + "; add the hook block (c copies it)"
		}
	}
	out = append(out, c)

	// MCP server
	c = Check{Name: "mcp", MCP: true}
	files := []string{opts.ClaudeJSON}
	if opts.ProjectDir != "" {
		files = append(files, filepath.Join(opts.ProjectDir, ".mcp.json"))
	}
	if where, state := mcpIn(files, opts); where != "" {
		c.OK, c.Text = true, tilde(where)+": total-recall mcp"
		if state != "" {
			c.Warn, c.Text = true, c.Text+", "+state
		}
	} else {
		c.Text = "not registered with Claude Code (optional); c copies the claude mcp add command"
	}
	out = append(out, c)

	// binary
	c = Check{Name: "binary"}
	path, err := opts.LookPath("total-recall")
	switch {
	case err != nil:
		c.Text = "total-recall is not on PATH; put it there or use an absolute path in the hooks"
	default:
		c.OK, c.Text = true, tilde(path)+" on PATH"
		exe, eerr := opts.Executable()
		switch {
		case eerr == nil && sameFile(exe, path):
			c.Text += ", this version (" + opts.Version + ")"
		default:
			v, verr := opts.VersionOf(path)
			switch {
			case verr != nil:
				c.Warn, c.Text = true, c.Text+", version unknown: "+verr.Error()
			case v != opts.Version:
				c.Warn, c.Text = true, c.Text+fmt.Sprintf(" is %s, this is %s", v, opts.Version)
			default:
				c.Text += ", this version (" + v + ")"
			}
		}
	}
	out = append(out, c)

	// backup directory
	c = Check{Name: "backups"}
	if fi, err := os.Stat(cfg.Backup.Dir); err == nil && fi.IsDir() || err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.OK, c.Text = true, tilde(cfg.Backup.Dir)+" exists"
		switch bk := store.ReadBackupStatus(store.BackupProbe{DB: cfg.DB, Dir: cfg.Backup.Dir, Loc: cfg.Location(), Now: now, Home: cfg.Home}); {
		case bk.State == store.BackupUnknown && bk.Path != "":
			c.Text = tilde(cfg.Backup.Dir) + " cannot be read (" + bk.Reason + "), last backup " + Age(now, bk.At) + " by the status file"
		case bk.State == store.BackupUnknown:
			c.Text = tilde(cfg.Backup.Dir) + " cannot be read (" + bk.Reason + "), last backup unknown"
		case bk.State == store.BackupOK:
			c.Text += ", last backup " + Age(now, bk.At)
		default:
			c.Warn, c.Text = true, c.Text+", no backup yet (actions: backup)"
		}
	} else {
		c.Text = tilde(cfg.Backup.Dir) + " does not exist"
		c.Fix = "create the directory"
		dir := cfg.Backup.Dir
		c.FixFn = func() error { return os.MkdirAll(dir, 0o755) }
	}
	out = append(out, c)

	// transcripts
	c = Check{Name: "transcripts"}
	switch {
	case !cfg.Harness.Claude.Enabled:
		c.OK, c.Warn, c.Text = true, true, "harness claude is disabled"
	default:
		if fi, err := os.Stat(cfg.Harness.Claude.ProjectsDir); err == nil && fi.IsDir() {
			c.OK, c.Text = true, tilde(cfg.Harness.Claude.ProjectsDir)
		} else {
			c.Text = tilde(cfg.Harness.Claude.ProjectsDir) + " not found; is Claude Code installed?"
		}
	}
	out = append(out, c)

	// archive
	c = Check{Name: "archive"}
	if _, err := os.Stat(cfg.DB); err == nil {
		c.OK, c.Text = true, tilde(cfg.DB)
	} else {
		c.Text = tilde(cfg.DB) + " missing; run ingest all (actions) or wait for the first hook"
	}
	out = append(out, c)
	return out
}

// hooksIn reads the Claude Code settings file and reports which events
// run total-recall: ingest on every event, the backup too on SessionEnd.
func hooksIn(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	out := map[string]bool{}
	for ev, matchers := range s.Hooks {
		ingest, backup := false, false
		for _, mt := range matchers {
			for _, h := range mt.Hooks {
				if strings.Contains(h.Command, "total-recall") && strings.Contains(h.Command, "ingest --hook") {
					ingest = true
				}
				if strings.Contains(h.Command, "total-recall") && strings.Contains(h.Command, "backup") {
					backup = true
				}
			}
		}
		out[ev] = ingest && (ev != "SessionEnd" || backup)
	}
	return out, nil
}

// mcpIn looks for the total-recall entry in the "mcpServers" object of the
// files, reading nothing else from them. It returns the file that has it and
// a hint when the entry points to another binary than the one on PATH, or
// "" for none.
func mcpIn(files []string, opts SetupEnv) (where, hint string) {
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var doc struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if json.Unmarshal(b, &doc) != nil {
			continue
		}
		for _, e := range doc.MCPServers {
			if len(e.Args) == 0 || e.Args[0] != "mcp" || filepath.Base(e.Command) != "total-recall" {
				continue
			}
			// A bare command resolves through PATH.
			cmd := e.Command
			if !strings.ContainsRune(cmd, filepath.Separator) {
				if cmd, err = opts.LookPath(cmd); err != nil {
					return f, "command not found on PATH"
				}
			}
			if _, err := os.Stat(cmd); err != nil {
				return f, "command " + tildeHome(cmd, opts.Home) + " does not exist"
			}
			return f, ""
		}
	}
	return "", ""
}

// sameFile reports whether two paths name the same file.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}
