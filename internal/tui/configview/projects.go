package configview

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/project"
)

// projectsPage shows the project definitions ([[project]]) in the order of
// the file: colour, name, description and patterns, read-only. Editing
// them in place (names, colours, patterns with their own input) is the
// second step of #4; until then E opens the file in the editor.
type projectsPage struct {
	row    int
	notice string
	bad    bool
}

func (projectsPage) title() string { return "projects" }

func (projectsPage) focused() bool { return false }

func (projectsPage) shortHelp(m *Model) []key.Binding {
	return []key.Binding{m.keys.Move, m.keys.Editor}
}

func (projectsPage) fullHelp(m *Model) [][]key.Binding {
	return [][]key.Binding{{m.keys.Up, m.keys.Down, m.keys.Editor, m.keys.Back}}
}

func (p projectsPage) reload(*Model) (page, tea.Cmd) { return p, nil }

func (p projectsPage) update(m *Model, msg tea.Msg) (page, tea.Cmd) {
	n := len(m.definitions())
	switch msg := msg.(type) {
	case editedMsg:
		var cmd tea.Cmd
		p.notice, p.bad, cmd = m.reloadFile(msg)
		return p, cmd
	case Changed:
		p.row = min(p.row, max(n-1, 0))
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			p.row = max(p.row-1, 0)
		case key.Matches(msg, m.keys.Down):
			p.row = min(p.row+1, max(n-1, 0))
		case key.Matches(msg, m.keys.Editor):
			notice, cmd := m.editor()
			if cmd == nil {
				p.notice, p.bad = notice, true
			}
			return p, cmd
		case key.Matches(msg, m.keys.Select):
			p.notice, p.bad = "projects are edited in the file for now (E opens $EDITOR)", false
		case key.Matches(msg, m.keys.Back):
			p.notice = ""
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && msg.Y >= 1 && msg.Y-1 < n {
			p.row = msg.Y - 1
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			p.row = max(p.row-1, 0)
		} else {
			p.row = min(p.row+1, max(n-1, 0))
		}
	}
	return p, nil
}

// definitions are the configured projects as the resolver sees them; nil
// while the table has a problem.
func (m *Model) definitions() []project.Definition {
	if m.opts.Config == nil {
		return nil
	}
	r, err := m.opts.Config.ProjectResolver()
	if err != nil {
		return nil
	}
	return r.Definitions()
}

// view renders one row per definition: a swatch in its colour, the name,
// the description and the patterns; under the rule the marked one in
// full.
func (p projectsPage) view(m *Model, width, height int) string {
	cfg := m.opts.Config
	if cfg == nil {
		return " " + m.th.Muted.Render("no configuration")
	}
	lines := make([]string, 0, height)
	if _, err := cfg.ProjectResolver(); err != nil {
		lines = append(lines, m.line(" "+m.th.Error.Render("[[project]] has a problem: "+err.Error()), width),
			m.line(" "+m.th.Muted.Render("fix it in the file (E opens $EDITOR); stats and worktime use the derived names meanwhile"), width))
		return strings.Join(lines, "\n")
	}
	defs := m.definitions()
	if len(defs) == 0 {
		lines = append(lines, " "+m.th.Muted.Render("no project definitions; sessions are named after their git root or directory"),
			" "+m.th.Muted.Render("[[project]] in the file gives a project its name, colour and patterns (E opens $EDITOR)"))
		return strings.Join(lines, "\n")
	}
	nameW, descW := 4, 11
	for _, d := range defs {
		nameW = max(nameW, lipgloss.Width(d.Name))
		descW = max(descW, lipgloss.Width(d.Description))
	}
	nameW, descW = min(nameW, 24), min(descW, 40)
	lines = append(lines, m.line(" "+m.th.Muted.Render("  "+pad("NAME", nameW)+"  "+pad("DESCRIPTION", descW)+"  MATCH"), width))
	for i, d := range defs {
		mark := " "
		if i == p.row {
			mark = m.th.Mark.Render("▸")
		}
		swatch := m.th.Muted.Render("·")
		if d.Color != "" {
			swatch = lipgloss.NewStyle().Foreground(lipgloss.Color(d.Color)).Render("■")
		}
		line := swatch + " " + pad(m.line(d.Name, nameW), nameW) + "  " + pad(m.line(d.Description, descW), descW) + "  " + strings.Join(d.Match, ", ")
		if i == p.row {
			line = m.th.Selected.Render(pad(m.line(line, width-2), width-2))
		}
		lines = append(lines, m.line(mark+" "+line, width))
	}
	for len(lines) < height-3 {
		lines = append(lines, "")
	}
	lines = append(lines, m.th.Rule.Render(strings.Repeat("─", width)))
	if p.row < len(defs) {
		d := defs[p.row]
		colour := "automatic colour"
		if d.Color != "" {
			colour = d.Color
		}
		lines = append(lines, m.line(" "+m.th.Label.Render(d.Name)+"  "+m.th.Muted.Render(colour)+"  "+strings.Join(d.Match, "  "), width))
	}
	switch {
	case p.notice != "" && p.bad:
		lines = append(lines, m.line(" "+m.th.Error.Render(p.notice), width))
	case p.notice != "":
		lines = append(lines, m.line(" "+m.th.Muted.Render(p.notice), width))
	default:
		lines = append(lines, m.line(" "+m.th.Muted.Render("first matching definition wins; edit them in the file, E opens $EDITOR"), width))
	}
	return strings.Join(lines, "\n")
}

func (projectsPage) cursor(int) *tea.Cursor { return nil }
