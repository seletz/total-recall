package tui

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/search"
	"github.com/seletz/total-recall/internal/tui/session"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/zone"
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
	// th is the theme of the chrome; dark until the terminal answers the
	// background colour query of Init.
	th theme.Theme
}

// Init starts every view and asks the terminal for its background colour;
// the answer reaches every view through Update.
func (a App) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(a.views)+2)
	for _, v := range a.views {
		cmds = append(cmds, v.Init())
	}
	return tea.Batch(append(cmds, a.detail.Init(), tea.RequestBackgroundColor)...)
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
	case tea.MouseClickMsg:
		return a.mouse(msg, func(y int) tea.Msg { msg.Y = y; return msg })
	case tea.MouseWheelMsg:
		return a.mouse(msg, func(y int) tea.Msg { msg.Y = y; return msg })
	case tea.MouseReleaseMsg, tea.MouseMotionMsg:
		return a, nil
	case search.OpenSession:
		a.showDetail = true
		return a.updateDetail(session.Open{SessionID: msg.SessionID, EventID: msg.EventID, Terms: msg.Terms})
	case session.Back:
		a.showDetail = false
		return a, nil
	case tea.BackgroundColorMsg:
		a.th = theme.New(msg.IsDark())
		return a, a.broadcast(msg)
	default:
		return a, a.broadcast(msg)
	}
}

func (a App) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// ctrl+c and a quit key with ctrl quit in every state, even while a
	// view holds the keyboard.
	if key.Matches(msg, a.keys.ForceQuit) || key.Matches(msg, a.keys.Quit) && msg.Mod.Contains(tea.ModCtrl) {
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
	}
	return a.forward(msg)
}

// mouse routes a click or wheel turn. A left click on a key hint in the
// footer presses that key, so it does exactly what the key does, in every
// state. While the help is open, a click outside its panel closes it and
// everything else is swallowed. A click on a tab switches the view (and
// closes the detail). Anything in the body goes to the current view with
// the header rows subtracted (shift builds the message with the new row);
// the rest of the chrome drops it.
func (a App) mouse(msg tea.MouseMsg, shift func(y int) tea.Msg) (tea.Model, tea.Cmd) {
	m := msg.Mouse()
	_, click := msg.(tea.MouseClickMsg)
	left := click && m.Button == tea.MouseLeft
	switch {
	case left && m.Y == a.height-1:
		if k, ok := a.hintAt(m.X); ok {
			return a.key(keys.Press(k))
		}
		return a, nil
	case a.help:
		if _, r := a.helpPanel(); left && !r.Contains(m.X, m.Y-headerRows) {
			a.help = false
		}
		return a, nil
	case left && m.Y == 0:
		if i := a.tabAt(m.X); i >= 0 {
			a.active, a.showDetail = i, false
		}
		return a, nil
	}
	if y := m.Y - headerRows; y >= 0 && y < a.bodySize().Height {
		return a.forward(shift(y))
	}
	return a, nil
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
	if !a.opts.NoMouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if a.width == 0 || a.height == 0 {
		return v
	}
	body := a.bodySize()
	bv := a.current().View()
	content := bv.Content
	if a.help {
		content, _ = a.helpPanel()
	} else if bv.Cursor != nil {
		// The view positions its cursor within the body; shift it below
		// the header and the rule.
		c := *bv.Cursor
		c.Y += headerRows
		v.Cursor = &c
	}
	bodyStyle := lipgloss.NewStyle().Width(body.Width).Height(body.Height).MaxWidth(body.Width).MaxHeight(body.Height)
	rule := a.th.Rule.Render(strings.Repeat("─", a.width))
	footer, _ := a.footer()
	v.Content = lipgloss.JoinVertical(lipgloss.Left,
		a.header(), rule, bodyStyle.Render(content), rule, footer)
	return v
}

// header is the program name on the left, in the gradient of the theme,
// and the view tabs on the right; the active tab is marked with brackets
// on the accent background, so it reads without colour too. While the
// detail is shown its title follows the active tab.
func (a App) header() string {
	left := " " + a.th.Gradient("total-recall")
	right := strings.Join(a.tabs(), " ") + " "
	gap := max(a.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// tabs renders the view tabs of the header: the number of an inactive tab
// in the accent, the active tab in brackets on the accent background.
func (a App) tabs() []string {
	tabs := make([]string, len(a.views))
	for i, v := range a.views {
		if i != a.active {
			tabs[i] = " " + a.th.Mark.Render(itoa(i+1)) + " " + v.Title() + " "
			continue
		}
		t := a.th.Active.Render("[" + itoa(i+1) + " " + v.Title() + "]")
		if a.showDetail {
			t += a.th.AccentText.Render(" › " + a.detail.Title())
		}
		tabs[i] = t
	}
	return tabs
}

// tabAt returns the index of the tab at column x of the header, -1 between
// and outside the tabs. It follows the layout of header: the tabs sit at
// the right edge, one space apart, with a trailing space.
func (a App) tabAt(x int) int {
	tabs := a.tabs()
	start := a.width - lipgloss.Width(strings.Join(tabs, " ")) - 1
	for i, t := range tabs {
		end := start + lipgloss.Width(t)
		if x >= start && x < end {
			return i
		}
		start = end + 1
	}
	return -1
}

// hint is one clickable part of a key hint in the footer: the key it
// presses and the columns it takes.
type hint struct {
	key  string
	span zone.Span
}

// footerSeparator stands between the key hints of the footer.
const footerSeparator = " • "

// footer renders the short help of the current view followed by the global
// keys as one line, and returns the hints with their columns, so a click
// lands on what is shown. The layout is that of the help widget's short
// help: key, space, description, the hints separated by footerSeparator;
// a hint that does not fit is replaced by an ellipsis unless not even the
// ellipsis fits, and the line is cut to the width in any case. Every key
// of a hint with several keys ("d/w/m period") is a part of its own and
// the description inert; a hint with one key is clickable as a whole.
func (a App) footer() (string, []hint) {
	st := a.th.Help()
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
	limit := a.width - 1
	sep := st.ShortSeparator.Inline(true).Render(footerSeparator)
	var b strings.Builder
	var hints []hint
	total := 0 // columns after the leading space
	for _, kb := range bindings {
		if !kb.Enabled() {
			continue
		}
		item := st.ShortKey.Inline(true).Render(kb.Help().Key) + " " + st.ShortDesc.Inline(true).Render(kb.Help().Desc)
		sepW := 0
		if total > 0 {
			item, sepW = sep+item, lipgloss.Width(sep)
		}
		w := lipgloss.Width(item)
		if limit > 0 && total+w > limit && total+2 < limit {
			b.WriteString(" " + st.Ellipsis.Inline(true).Render("…"))
			break
		}
		hints = append(hints, hintsOf(kb, 1+total+sepW, 1+total+w)...)
		total += w
		b.WriteString(item)
	}
	line := " " + b.String()
	if lipgloss.Width(line) > a.width {
		// The ellipsis takes the last column.
		line = ansi.Truncate(line, a.width, "…")
		for i := range hints {
			hints[i].span = hints[i].span.Clip(max(a.width-1, 0))
		}
	}
	return line, hints
}

// hintsOf splits one key hint into its clickable parts between the
// columns start and end: the keys it names, one after the other, and the
// description (which presses the key when there is only one).
func hintsOf(kb key.Binding, start, end int) []hint {
	shown := keys.Shown(kb)
	if len(shown) == 0 {
		return nil
	}
	if len(shown) == 1 {
		return []hint{{key: shown[0], span: zone.Span{Start: start, End: end}}}
	}
	out := make([]hint, len(shown))
	x := start
	for i, k := range shown {
		w := lipgloss.Width(keys.Display(k))
		out[i] = hint{key: k, span: zone.Span{Start: x, End: x + w}}
		x += w + 1 // the slash
	}
	return out
}

// hintAt returns the key of the hint at column x of the footer.
func (a App) hintAt(x int) (string, bool) {
	_, hints := a.footer()
	for _, h := range hints {
		if h.span.Contains(x) {
			return h.key, true
		}
	}
	return "", false
}

// helpPanel renders every binding of the current view and the app as a
// centred panel with a rounded border in the accent colour, and returns
// the body content and the rectangle the panel takes in it.
func (a App) helpPanel() (string, zone.Rect) {
	h := help.New()
	h.Styles = a.th.Help()
	h.ShowAll = true
	groups := append(a.current().FullHelp(), a.keys.FullHelp()...)
	panel := lipgloss.JoinVertical(lipgloss.Left,
		a.th.Title.Render("keys: "+a.current().Title()), "", h.FullHelpView(groups), "",
		a.th.Muted.Render(a.keys.Back.Help().Key+" or "+a.keys.Help.Help().Key+" closes this help"))
	box := a.th.Border.Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(panel)
	body := a.bodySize()
	// lipgloss.Place centres with the smaller half of the gap in front.
	r := zone.Rect{W: lipgloss.Width(box), H: lipgloss.Height(box)}
	r.X, r.Y = max(body.Width-r.W, 0)/2, max(body.Height-r.H, 0)/2
	return lipgloss.Place(body.Width, body.Height, lipgloss.Center, lipgloss.Center, box), r
}

func itoa(i int) string { return string(rune('0' + i)) }
