// Package search is the live full-text search view of the TUI. This is the
// placeholder of the app shell; input, hit list and preview follow.
package search

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
)

// Model is the search view.
type Model struct {
	width, height int
}

// New returns an empty search view.
func New() Model { return Model{} }

// Title implements tui.View.
func (Model) Title() string { return "search" }

// Init implements tea.Model.
func (Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = s.Width, s.Height
	}
	return m, nil
}

// View implements tea.Model.
func (Model) View() tea.View { return tea.NewView(" search: nothing here yet") }

// ShortHelp implements help.KeyMap.
func (Model) ShortHelp() []key.Binding { return nil }

// FullHelp implements help.KeyMap.
func (Model) FullHelp() [][]key.Binding { return nil }
