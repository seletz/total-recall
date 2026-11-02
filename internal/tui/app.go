package tui

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/tui/search"
	"github.com/seletz/total-recall/internal/tui/session"
)

// Chrome rows: header line and rule above the body, rule and footer below.
const (
	chromeHeight = 4
	headerRows   = chromeHeight / 2
)

// App is the root model: header with view tabs, the active view's body (or
// the session detail above it), footer with key hints, and a help overlay.
type App struct {
	opts   Options
	views  []View
	active int
	// detail is the session view; it is shown instead of the active view
	// while showDetail is set and keeps the keyboard then.
	detail     session.Model
	showDetail bool
	keys       KeyMap
	help       bool
	width      int
	height     int
}

// Init starts every view.
func (a App) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(a.views)+1)
	for _, v := range a.views {
		cmds = append(cmds, v.Init())
	}
	return tea.Batch(append(cmds, a.detail.Init())...)
}

// Update handles global keys and forwards the rest: key presses to the active
// view (or the detail), everything else to all views (results of background
// commands must reach views that are not shown). OpenSession from the search
// view opens the detail, Back from the detail closes it.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, a.broadcast(a.bodySize())
	case tea.KeyPressMsg:
		return a.key(msg)
	case search.OpenSession:
		a.showDetail = true
		return a.updateDetail(session.Open{SessionID: msg.SessionID, EventID: msg.EventID, Terms: msg.Terms})
	case session.Back:
		a.showDetail = false
		return a, nil
	default:
		return a, a.broadcast(msg)
	}
}

func (a App) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.Quit) && msg.Mod.Contains(tea.ModCtrl) {
		return a, tea.Quit
	}
	if a.help {
		switch {
		case key.Matches(msg, a.keys.Help, a.keys.Back):
			a.help = false
		case key.Matches(msg, a.keys.Quit):
			return a, tea.Quit
		}
		return a, nil
	}
	if f, ok := a.current().(Focused); ok && f.Focused() {
		return a.forward(msg)
	}
	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit
	case key.Matches(msg, a.keys.Help):
		a.help = true
		return a, nil
	case key.Matches(msg, a.keys.Search):
		a.active, a.showDetail = 0, false
		return a, nil
	case key.Matches(msg, a.keys.Stats):
		a.active, a.showDetail = 1, false
		return a, nil
	case key.Matches(msg, a.keys.Activity):
		a.active, a.showDetail = 2, false
		return a, nil
	case key.Matches(msg, a.keys.Next):
		a.active, a.showDetail = (a.active+1)%len(a.views), false
		return a, nil
	}
	return a.forward(msg)
}

// current is the view that owns the body: the detail while it is shown,
// else the active view.
func (a App) current() View {
	if a.showDetail {
		return a.detail
	}
	return a.views[a.active]
}

// forward passes msg to the current view only.
func (a App) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.showDetail {
		return a.updateDetail(msg)
	}
	m, cmd := a.views[a.active].Update(msg)
	a.views = a.replace(a.active, m)
	return a, cmd
}

// updateDetail passes msg to the session view.
func (a App) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.detail.Update(msg)
	a.detail = m.(session.Model)
	return a, cmd
}

// broadcast passes msg to every view and the detail.
func (a *App) broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(a.views)+1)
	for i, v := range a.views {
		m, cmd := v.Update(msg)
		a.views = a.replace(i, m)
		cmds[i] = cmd
	}
	m, cmd := a.detail.Update(msg)
	a.detail, cmds[len(a.views)] = m.(session.Model), cmd
	return tea.Batch(cmds...)
}

// replace returns a copy of the view slice with views[i] set to m, so the
// value-receiver model never mutates a slice shared with an older copy.
func (a App) replace(i int, m tea.Model) []View {
	views := make([]View, len(a.views))
	copy(views, a.views)
	views[i] = m.(View)
	return views
}

// bodySize is the window size the views see: the terminal minus the chrome.
func (a App) bodySize() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: a.width, Height: max(a.height-chromeHeight, 0)}
}

// View composes header, body (or help overlay) and footer.
func (a App) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	if a.width == 0 || a.height == 0 {
		return v
	}
	body := a.bodySize()
	bv := a.current().View()
	content := bv.Content
	if a.help {
		content = a.helpView()
	} else if bv.Cursor != nil {
		// The view positions its cursor within the body; shift it below
		// the header and the rule.
		c := *bv.Cursor
		c.Y += headerRows
		v.Cursor = &c
	}
	bodyStyle := lipgloss.NewStyle().Width(body.Width).Height(body.Height).MaxWidth(body.Width).MaxHeight(body.Height)
	rule := strings.Repeat("─", a.width)
	v.Content = lipgloss.JoinVertical(lipgloss.Left,
		a.header(), rule, bodyStyle.Render(content), rule, a.footer())
	return v
}

var (
	titleStyle     = lipgloss.NewStyle().Bold(true)
	activeTabStyle = lipgloss.NewStyle().Reverse(true)
	faintStyle     = lipgloss.NewStyle().Faint(true)
)

// header is the program name on the left and the view tabs on the right; the
// active tab is marked with brackets so it reads without colour. While the
// detail is shown its title follows the active tab.
func (a App) header() string {
	tabs := make([]string, len(a.views))
	for i, v := range a.views {
		t := " " + itoa(i+1) + " " + v.Title() + " "
		if i == a.active {
			t = activeTabStyle.Render("[" + strings.TrimSpace(t) + "]")
			if a.showDetail {
				t += " › " + a.detail.Title()
			}
		}
		tabs[i] = t
	}
	left := titleStyle.Render(" total-recall")
	right := strings.Join(tabs, " ") + " "
	gap := max(a.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// footer shows the short help of the current view followed by the global
// keys. The help widget drops items that do not fit, but keeps adding them
// once there is no room for its ellipsis either, so the line is cut here
// as well.
func (a App) footer() string {
	h := help.New()
	h.SetWidth(a.width - 1)
	bindings := a.keys.ShortHelp()
	switch f, ok := a.current().(Focused); {
	case a.help:
		bindings = []key.Binding{a.keys.Back, a.keys.Quit}
	case ok && f.Focused():
		// The view takes every key but ctrl+c, so only that one is global.
		bindings = append(a.current().ShortHelp(), a.keys.ForceQuit)
	default:
		bindings = append(a.current().ShortHelp(), bindings...)
	}
	return ansi.Truncate(" "+h.ShortHelpView(bindings), a.width, "…")
}

// helpView renders every binding of the current view and the app as a
// centred panel.
func (a App) helpView() string {
	h := help.New()
	h.ShowAll = true
	groups := append(a.current().FullHelp(), a.keys.FullHelp()...)
	panel := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("keys: "+a.current().Title()), "", h.FullHelpView(groups), "",
		faintStyle.Render("esc or ? closes this help"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(panel)
	body := a.bodySize()
	return lipgloss.Place(body.Width, body.Height, lipgloss.Center, lipgloss.Center, box)
}

func itoa(i int) string { return string(rune('0' + i)) }
