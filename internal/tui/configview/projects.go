package configview

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// projectsPage is the place of the project definitions (#4): names,
// colours and patterns from [[project]]. Until they exist it says so.
type projectsPage struct{}

func (projectsPage) title() string                            { return "projects" }
func (p projectsPage) update(*Model, tea.Msg) (page, tea.Cmd) { return p, nil }
func (p projectsPage) reload(*Model) (page, tea.Cmd)          { return p, nil }
func (projectsPage) focused() bool                            { return false }
func (projectsPage) shortHelp(*Model) []key.Binding           { return nil }
func (projectsPage) fullHelp(*Model) [][]key.Binding          { return nil }
func (projectsPage) view(m *Model, width, _ int) string {
	return m.line(" "+m.th.Muted.Render("project definitions ([[project]]: name, colour, patterns) come with a later version"), width)
}

func (projectsPage) cursor(int) *tea.Cursor { return nil }
