package configview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/store"
)

// Signs of the checks: ok, ok with a hint, failed. Glyphs of the WGL4
// set, so the embedded font of the screenshots has them.
const (
	signOK   = "√"
	signWarn = "!"
	signFail = "×"
)

// check is one line of the setup page.
type check struct {
	name string
	// ok is a tick, warn a hint on an ok line; neither is a cross.
	ok, warn bool
	text     string
	// fix, when set, is offered on select and run by fixFn after a
	// confirmation; reloadConfig says the configuration changed by it.
	// mcp marks the line whose block (the MCP entry) is shown and copied
	// instead of the hook block while it is marked.
	mcp          bool
	fix          string
	fixFn        func() error
	reloadConfig bool
}

// checksMsg carries the result of the setup checks.
type checksMsg struct {
	seq    uint64
	checks []check
}

// fixedMsg is the outcome of a fix.
type fixedMsg struct {
	name string
	err  error
	cfg  *config.Config
}

// setupPage checks the installation and shows the hook block.
type setupPage struct {
	checks  []check
	seq     uint64
	loading bool
	row     int
	confirm bool
	notice  string
	bad     bool
}

func newSetupPage() setupPage { return setupPage{} }

func (setupPage) title() string { return "setup" }

func (setupPage) focused() bool { return false }

func (p setupPage) shortHelp(m *Model) []key.Binding {
	if p.confirm {
		return []key.Binding{m.keys.Confirm, m.keys.Back}
	}
	return []key.Binding{m.keys.Move, m.keys.Select, m.keys.Copy}
}

func (p setupPage) fullHelp(m *Model) [][]key.Binding {
	return [][]key.Binding{{m.keys.Up, m.keys.Down, m.keys.Select}, {m.keys.Copy, m.keys.Confirm, m.keys.Back}}
}

// reload runs the checks again.
func (p setupPage) reload(m *Model) (page, tea.Cmd) {
	p.seq++
	p.loading = true
	seq, opts, now := p.seq, m.opts, m.opts.Now()
	return p, func() tea.Msg {
		return checksMsg{seq: seq, checks: runChecks(opts, now)}
	}
}

func (p setupPage) update(m *Model, msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case checksMsg:
		if msg.seq != p.seq {
			return p, nil
		}
		p.loading, p.checks = false, msg.checks
		p.row = min(p.row, max(len(p.checks)-1, 0))
		return p, nil
	case fixedMsg:
		if msg.err != nil {
			p.notice, p.bad = msg.name+": "+msg.err.Error(), true
			return p, nil
		}
		p.notice, p.bad = msg.name+": done", false
		np, cmd := p.reload(m)
		if msg.cfg != nil {
			return np, tea.Batch(cmd, func() tea.Msg { return Changed{Config: msg.cfg} })
		}
		return np, cmd
	case stateMsg, Changed:
		return p.reload(m)
	case tea.KeyPressMsg:
		return p.key(m, msg)
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || p.confirm {
			return p, nil
		}
		i := msg.Y - 1
		if i < 0 || i >= len(p.checks) {
			return p, nil
		}
		if i == p.row {
			return p.selectRow(m)
		}
		p.row = i
		return p, nil
	}
	return p, nil
}

func (p setupPage) key(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	if p.confirm {
		switch {
		case key.Matches(msg, m.keys.Confirm):
			p.confirm = false
			return p.fix(m)
		case key.Matches(msg, m.keys.Back):
			p.confirm = false
			p.notice = ""
		}
		return p, nil
	}
	switch {
	case key.Matches(msg, m.keys.Up):
		p.row = max(p.row-1, 0)
		p.notice = ""
	case key.Matches(msg, m.keys.Down):
		p.row = min(p.row+1, max(len(p.checks)-1, 0))
		p.notice = ""
	case key.Matches(msg, m.keys.Select):
		return p.selectRow(m)
	case key.Matches(msg, m.keys.Copy):
		if p.onMCP() {
			p.notice, p.bad = "claude mcp add command copied to the clipboard", false
			return p, tea.SetClipboard(ops.ClaudeMCPAddCommand("total-recall") + "\n")
		}
		p.notice, p.bad = "hook block copied to the clipboard", false
		return p, tea.SetClipboard(hookBlock(m) + "\n")
	case key.Matches(msg, m.keys.Back):
		p.notice = ""
	}
	return p, nil
}

// selectRow offers the fix of the marked check.
func (p setupPage) selectRow(m *Model) (page, tea.Cmd) {
	if p.row >= len(p.checks) {
		return p, nil
	}
	c := p.checks[p.row]
	if c.fix == "" {
		if c.ok {
			p.notice, p.bad = c.name+": nothing to do", false
		} else {
			p.notice, p.bad = c.name+": no fix from here, see the text", true
		}
		return p, nil
	}
	p.confirm = true
	return p, nil
}

// fix runs the fix of the marked check.
func (p setupPage) fix(m *Model) (page, tea.Cmd) {
	c := p.checks[p.row]
	cfg := m.opts.Config
	return p, func() tea.Msg {
		if err := c.fixFn(); err != nil {
			return fixedMsg{name: c.name, err: err}
		}
		if !c.reloadConfig || cfg == nil {
			return fixedMsg{name: c.name}
		}
		n, err := cfg.Reload()
		return fixedMsg{name: c.name, err: err, cfg: n}
	}
}

// onMCP says whether the marked line is the MCP check.
func (p setupPage) onMCP() bool { return p.row < len(p.checks) && p.checks[p.row].mcp }

// mcpBlock is the command and the JSON entry of the MCP server.
func mcpBlock() string {
	b, err := json.MarshalIndent(ops.ClaudeMCPConfig("total-recall"), "", "  ")
	if err != nil {
		return err.Error()
	}
	return ops.ClaudeMCPAddCommand("total-recall") + "\n\nor in .mcp.json:\n" + string(b)
}

// hookBlock is the settings.json block for the binary as found.
func hookBlock(m *Model) string {
	b, err := json.MarshalIndent(ops.ClaudeHooks("total-recall"), "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// view renders the checks, a rule, the notice and the hook block.
func (p setupPage) view(m *Model, width, height int) string {
	if m.opts.Config == nil {
		return " " + m.th.Muted.Render("no configuration")
	}
	lines := make([]string, 0, height)
	lines = append(lines, m.line(" "+m.th.Muted.Render("CHECK        STATE"), width))
	if p.loading && len(p.checks) == 0 {
		lines = append(lines, " "+m.th.Muted.Render("checking…"))
	}
	for i, c := range p.checks {
		mark := " "
		if i == p.row {
			mark = m.th.Mark.Render("▸")
		}
		st := newRowStyle(m.th, i == p.row)
		sign := st.on(m.th.Warn).Render(signFail)
		switch {
		case c.ok && c.warn:
			sign = st.on(m.th.Warn).Render(signWarn)
		case c.ok:
			sign = st.on(m.th.Accent).Render(signOK)
		}
		text := st.text.Render(c.text)
		if c.fix != "" {
			text += st.text.Render("  ") + st.on(m.th.Accent).Render("enter: "+c.fix)
		}
		line := st.text.Render(pad(c.name, 11)+" ") + sign + st.text.Render(" ") + text
		lines = append(lines, m.line(mark+" "+st.fill(m.line(line, width-2), width-2), width))
	}
	lines = append(lines, m.th.Rule.Render(strings.Repeat("─", width)))
	switch {
	case p.confirm && p.row < len(p.checks):
		lines = append(lines, m.line(" "+m.th.Strong.Render(p.checks[p.row].name+":")+" "+p.checks[p.row].fix+"? "+m.th.AccentText.Render("y confirms, esc cancels"), width))
	case p.notice != "" && p.bad:
		lines = append(lines, m.line(" "+m.th.Error.Render(p.notice), width))
	case p.notice != "":
		lines = append(lines, m.line(" "+m.th.Muted.Render(p.notice), width))
	default:
		if p.onMCP() {
			lines = append(lines, m.line(" "+m.th.Muted.Render("MCP server entry for Claude Code, nothing is written; c copies the command"), width))
		} else {
			lines = append(lines, m.line(" "+m.th.Muted.Render("hook block for "+m.tilde(m.opts.SettingsFile)+"; c copies it, enter offers a fix where there is one"), width))
		}
	}
	block := hookBlock(m)
	if p.onMCP() {
		block = mcpBlock()
	}
	for _, l := range strings.Split(block, "\n") {
		if len(lines) >= height {
			break
		}
		lines = append(lines, m.line("   "+m.th.Muted.Render(l), width))
	}
	return strings.Join(lines, "\n")
}

// runChecks examines the installation.
func runChecks(opts Options, now time.Time) []check {
	cfg := opts.Config
	if cfg == nil {
		return nil
	}
	tilde := func(p string) string { return Model{opts: opts}.tilde(p) }
	var out []check

	// config file
	c := check{name: "config"}
	switch {
	case cfg.Loaded:
		c.ok, c.text = true, tilde(cfg.Path)+" loaded"
		if n := len(cfg.Problems()); n > 0 {
			c.warn, c.text = true, c.text+fmt.Sprintf(", %s (see status)", plural(n, "problem"))
		}
	default:
		c.text = tilde(cfg.Path) + " missing (defaults apply)"
		c.fix, c.reloadConfig = "write template", true
		path := cfg.Path
		c.fixFn = func() error { return config.WriteTemplate(path) }
	}
	out = append(out, c)

	// hooks
	c = check{name: "hooks"}
	present, err := hooksIn(opts.SettingsFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.text = tilde(opts.SettingsFile) + " not found; add the hook block (c copies it)"
	case err != nil:
		c.text = tilde(opts.SettingsFile) + ": " + err.Error()
	default:
		var missing []string
		for _, ev := range ops.HookEvents {
			if !present[ev] {
				missing = append(missing, ev)
			}
		}
		if len(missing) == 0 {
			c.ok, c.text = true, tilde(opts.SettingsFile)+": "+strings.Join(ops.HookEvents, ", ")
		} else {
			c.text = tilde(opts.SettingsFile) + " lacks " + strings.Join(missing, ", ") + "; add the hook block (c copies it)"
		}
	}
	out = append(out, c)

	// MCP server
	c = check{name: "mcp", mcp: true}
	files := []string{opts.ClaudeJSON}
	if opts.ProjectDir != "" {
		files = append(files, filepath.Join(opts.ProjectDir, ".mcp.json"))
	}
	if where, state := mcpIn(files, opts); where != "" {
		c.ok, c.text = true, tilde(where)+": total-recall mcp"
		if state != "" {
			c.warn, c.text = true, c.text+", "+state
		}
	} else {
		c.text = "not registered with Claude Code (optional); c copies the claude mcp add command"
	}
	out = append(out, c)

	// binary
	c = check{name: "binary"}
	path, err := opts.LookPath("total-recall")
	switch {
	case err != nil:
		c.text = "total-recall is not on PATH; put it there or use an absolute path in the hooks"
	default:
		c.ok, c.text = true, tilde(path)+" on PATH"
		exe, eerr := opts.Executable()
		switch {
		case eerr == nil && sameFile(exe, path):
			c.text += ", this version (" + opts.Version + ")"
		default:
			v, verr := opts.VersionOf(path)
			switch {
			case verr != nil:
				c.warn, c.text = true, c.text+", version unknown: "+verr.Error()
			case v != opts.Version:
				c.warn, c.text = true, c.text+fmt.Sprintf(" is %s, this is %s", v, opts.Version)
			default:
				c.text += ", this version (" + v + ")"
			}
		}
	}
	out = append(out, c)

	// backup directory
	c = check{name: "backups"}
	if fi, err := os.Stat(cfg.Backup.Dir); err == nil && fi.IsDir() || err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.ok, c.text = true, tilde(cfg.Backup.Dir)+" exists"
		switch bk := store.ReadBackupStatus(cfg.DB, cfg.Backup.Dir, cfg.Location()); {
		case bk.State == store.BackupUnknown && bk.Path != "":
			c.text = tilde(cfg.Backup.Dir) + " cannot be read (" + bk.Reason + "), last backup " + age(now, bk.At) + " by the status file"
		case bk.State == store.BackupUnknown:
			c.text = tilde(cfg.Backup.Dir) + " cannot be read (" + bk.Reason + "), last backup unknown"
		case bk.State == store.BackupOK:
			c.text += ", last backup " + age(now, bk.At)
		default:
			c.warn, c.text = true, c.text+", no backup yet (actions: backup)"
		}
	} else {
		c.text = tilde(cfg.Backup.Dir) + " does not exist"
		c.fix = "create the directory"
		dir := cfg.Backup.Dir
		c.fixFn = func() error { return os.MkdirAll(dir, 0o755) }
	}
	out = append(out, c)

	// transcripts
	c = check{name: "transcripts"}
	switch {
	case !cfg.Harness.Claude.Enabled:
		c.ok, c.warn, c.text = true, true, "harness claude is disabled"
	default:
		if fi, err := os.Stat(cfg.Harness.Claude.ProjectsDir); err == nil && fi.IsDir() {
			c.ok, c.text = true, tilde(cfg.Harness.Claude.ProjectsDir)
		} else {
			c.text = tilde(cfg.Harness.Claude.ProjectsDir) + " not found; is Claude Code installed?"
		}
	}
	out = append(out, c)

	// archive
	c = check{name: "archive"}
	if _, err := os.Stat(cfg.DB); err == nil {
		c.ok, c.text = true, tilde(cfg.DB)
	} else {
		c.text = tilde(cfg.DB) + " missing; run ingest all (actions) or wait for the first hook"
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
func mcpIn(files []string, opts Options) (where, hint string) {
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
				return f, "command " + Model{opts: opts}.tilde(cmd) + " does not exist"
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

func (setupPage) cursor(int) *tea.Cursor { return nil }
