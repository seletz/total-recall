package configview

import (
	"encoding/json"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/ops"
)

// Signs of the checks: ok, ok with a hint, failed. Glyphs of the WGL4
// set, so the embedded font of the screenshots has them.
const (
	signOK   = "√"
	signWarn = "!"
	signFail = "×"
)

// check is one line of the setup page: the shared setup check.
type check = ops.Check

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
		return checksMsg{seq: seq, checks: ops.SetupChecks(opts.setupEnv(), now)}
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
	if c.Fix == "" {
		if c.OK {
			p.notice, p.bad = c.Name+": nothing to do", false
		} else {
			p.notice, p.bad = c.Name+": no fix from here, see the text", true
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
		if err := c.FixFn(); err != nil {
			return fixedMsg{name: c.Name, err: err}
		}
		if !c.ReloadConfig || cfg == nil {
			return fixedMsg{name: c.Name}
		}
		n, err := cfg.Reload()
		return fixedMsg{name: c.Name, err: err, cfg: n}
	}
}

// onMCP says whether the marked line is the MCP check.
func (p setupPage) onMCP() bool { return p.row < len(p.checks) && p.checks[p.row].MCP }

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
		case c.OK && c.Warn:
			sign = st.on(m.th.Warn).Render(signWarn)
		case c.OK:
			sign = st.on(m.th.Accent).Render(signOK)
		}
		text := st.text.Render(c.Text)
		if c.Fix != "" {
			text += st.text.Render("  ") + st.on(m.th.Accent).Render("enter: "+c.Fix)
		}
		line := st.text.Render(pad(c.Name, 11)+" ") + sign + st.text.Render(" ") + text
		lines = append(lines, m.line(mark+" "+st.fill(m.line(line, width-2), width-2), width))
	}
	lines = append(lines, m.th.Rule.Render(strings.Repeat("─", width)))
	switch {
	case p.confirm && p.row < len(p.checks):
		lines = append(lines, m.line(" "+m.th.Strong.Render(p.checks[p.row].Name+":")+" "+p.checks[p.row].Fix+"? "+m.th.AccentText.Render("y confirms, esc cancels"), width))
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

func (setupPage) cursor(int) *tea.Cursor { return nil }

// setupEnv is what the shared setup checks look at.
func (o Options) setupEnv() ops.SetupEnv {
	return ops.SetupEnv{
		Config: o.Config, Home: o.Home, Version: o.Version, Getenv: o.Getenv,
		SettingsFile: o.SettingsFile, ClaudeJSON: o.ClaudeJSON, ProjectDir: o.ProjectDir,
		LookPath: o.LookPath, Executable: o.Executable, VersionOf: o.VersionOf,
	}
}
