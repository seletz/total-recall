package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/search"
	"github.com/seletz/total-recall/internal/tui/session"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// Messages of the reload cycle: tickMsg says the interval is over,
// pollMsg carries what the store found, reopenMsg the outcome of opening
// a swapped file again.
type (
	tickMsg struct{}
	pollMsg struct {
		change store.Change
		err    error
	}
	reopenMsg struct{ err error }
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
	// stale marks the views (and, last, the detail) whose data is older
	// than the archive: they reload when they are shown next. updated is
	// when the data on screen was last loaded, shown in the header;
	// reloadErr is the error of the last check or reload, if any.
	stale     []bool
	updated   time.Time
	reloadErr error
}

// Init starts every view, asks the terminal for its background colour (the
// answer reaches every view through Update) and starts the reload cycle.
func (a App) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(a.views)+3)
	for _, v := range a.views {
		cmds = append(cmds, v.Init())
	}
	return tea.Batch(append(cmds, a.detail.Init(), tea.RequestBackgroundColor, a.tick())...)
}

// tick schedules the next change check, or nothing when the reload is off
// or there is no store.
func (a App) tick() tea.Cmd {
	if a.opts.Reload <= 0 || a.opts.Store == nil {
		return nil
	}
	return a.opts.Tick(a.opts.Reload, func(time.Time) tea.Msg { return tickMsg{} })
}

// poll asks the store whether the archive changed.
func (a App) poll() tea.Cmd {
	st := a.opts.Store
	return func() tea.Msg {
		c, err := st.Poll(context.Background())
		return pollMsg{change: c, err: err}
	}
}

// reopen opens the swapped archive file again.
func (a App) reopen() tea.Cmd {
	st := a.opts.Store
	return func() tea.Msg { return reopenMsg{err: st.Reopen()} }
}

// polled acts on the outcome of a check: a modified archive reloads the
// view on screen and marks the others stale, a replaced file is reopened
// first (its result reloads), an error is shown in the header. The next
// check is scheduled in every case except while a reopen is pending.
func (a App) polled(msg pollMsg) (tea.Model, tea.Cmd) {
	a.reloadErr = msg.err
	switch {
	case msg.err != nil:
		return a, a.tick()
	case msg.change == store.Replaced:
		return a, a.reopen()
	case msg.change == store.Modified:
		cmd := a.reload()
		return a, tea.Batch(cmd, a.tick())
	}
	return a, a.tick()
}

// reopened reloads everything after the file was opened again; on an
// error the old data stays and the header says so.
func (a App) reopened(msg reopenMsg) (tea.Model, tea.Cmd) {
	a.reloadErr = msg.err
	if msg.err != nil {
		return a, a.tick()
	}
	cmd := a.reload()
	return a, tea.Batch(cmd, a.tick())
}

// reload loads the view on screen again and marks every other one stale;
// the header shows the time.
func (a *App) reload() tea.Cmd {
	a.stale = make([]bool, len(a.views)+1)
	for i := range a.stale {
		a.stale[i] = true
	}
	a.updated = a.opts.Now()
	return a.reloadCurrent()
}

// reloadCurrent reloads the view on screen (the detail while it is shown)
// and clears its stale mark.
func (a *App) reloadCurrent() tea.Cmd {
	if a.showDetail {
		a.setStale(len(a.views), false)
		m, cmd := a.detail.Reload()
		a.detail = m.(session.Model)
		return cmd
	}
	a.setStale(a.active, false)
	m, cmd := a.views[a.active].Reload()
	a.views = a.replace(a.active, m)
	return cmd
}

// setStale sets the stale mark of view i on a copy of the slice, so the
// value-receiver model never mutates a slice shared with an older copy.
func (a *App) setStale(i int, v bool) {
	stale := make([]bool, len(a.stale))
	copy(stale, a.stale)
	stale[i] = v
	a.stale = stale
}

// show switches to view i, closing the detail, and reloads the view when
// its data went stale while it was hidden.
func (a *App) show(i int) tea.Cmd {
	a.active, a.showDetail = i, false
	if !a.stale[i] {
		return nil
	}
	return a.reloadCurrent()
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
		a.setStale(len(a.views), false) // Open loads afresh
		return a.updateDetail(session.Open{SessionID: msg.SessionID, EventID: msg.EventID, Terms: msg.Terms})
	case session.Back:
		return a, a.show(a.active)
	case tickMsg:
		if a.opts.Store == nil {
			return a, nil
		}
		return a, a.poll()
	case pollMsg:
		return a.polled(msg)
	case reopenMsg:
		return a.reopened(msg)
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
		return a, a.show(0)
	case key.Matches(msg, a.keys.Stats):
		return a, a.show(1)
	case key.Matches(msg, a.keys.Activity):
		return a, a.show(2)
	case key.Matches(msg, a.keys.Reload):
		if a.opts.Store == nil {
			return a, nil
		}
		a.reloadErr = nil
		return a, a.reload()
	}
	return a.forward(msg)
}

// mouse routes a click or wheel turn: the help overlay swallows it, a click
// on a tab switches the view (and closes the detail), anything in the body
// goes to the current view with the header rows subtracted (shift builds
// the message with the new row), the rest of the chrome drops it.
func (a App) mouse(msg tea.MouseMsg, shift func(y int) tea.Msg) (tea.Model, tea.Cmd) {
	if a.help {
		return a, nil
	}
	m := msg.Mouse()
	if _, click := msg.(tea.MouseClickMsg); click && m.Y == 0 && m.Button == tea.MouseLeft {
		if i := a.tabAt(m.X); i >= 0 {
			return a, a.show(i)
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
		content = a.helpView()
	} else if bv.Cursor != nil {
		// The view positions its cursor within the body; shift it below
		// the header and the rule.
		c := *bv.Cursor
		c.Y += headerRows
		v.Cursor = &c
	}
	bodyStyle := lipgloss.NewStyle().Width(body.Width).Height(body.Height).MaxWidth(body.Width).MaxHeight(body.Height)
	rule := a.th.Rule.Render(strings.Repeat("─", a.width))
	v.Content = lipgloss.JoinVertical(lipgloss.Left,
		a.header(), rule, bodyStyle.Render(content), rule, a.footer())
	return v
}

// header is the program name on the left, in the gradient of the theme,
// the time of the last load (muted; or the reload error) in the middle
// and the view tabs on the right; the active tab is marked with brackets
// on the accent background, so it reads without colour too. While the
// detail is shown its title follows the active tab. The time gives way to
// the tabs in a narrow window.
func (a App) header() string {
	left := " " + a.th.Gradient("total-recall")
	right := strings.Join(a.tabs(), " ") + " "
	gap := max(a.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	if status := a.reloadStatus(); status != "" && lipgloss.Width(status)+4 <= gap {
		pad := (gap - lipgloss.Width(status)) / 2
		return left + strings.Repeat(" ", pad) + status + strings.Repeat(" ", gap-pad-lipgloss.Width(status)) + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// reloadStatus is the middle of the header: "updated 14:03:12" in the
// time zone of the views, or the last reload error; empty without a
// store.
func (a App) reloadStatus() string {
	switch {
	case a.opts.Store == nil:
		return ""
	case a.reloadErr != nil:
		return a.th.Error.Render(ansi.Truncate("reload: "+a.reloadErr.Error(), max(a.width/2, 1), "…"))
	}
	return a.th.Muted.Render("updated " + a.updated.In(a.opts.Location).Format("15:04:05"))
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

// footer shows the short help of the current view followed by the global
// keys. The help widget drops items that do not fit, but keeps adding them
// once there is no room for its ellipsis either, so the line is cut here
// as well.
func (a App) footer() string {
	h := help.New()
	h.Styles = a.th.Help()
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
// centred panel with a rounded border in the accent colour.
func (a App) helpView() string {
	h := help.New()
	h.Styles = a.th.Help()
	h.ShowAll = true
	groups := append(a.current().FullHelp(), a.keys.FullHelp()...)
	panel := lipgloss.JoinVertical(lipgloss.Left,
		a.th.Title.Render("keys: "+a.current().Title()), "", h.FullHelpView(groups), "",
		a.th.Muted.Render(a.keys.Back.Help().Key+" or "+a.keys.Help.Help().Key+" closes this help"))
	box := a.th.Border.Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(panel)
	body := a.bodySize()
	return lipgloss.Place(body.Width, body.Height, lipgloss.Center, lipgloss.Center, box)
}

func itoa(i int) string { return string(rune('0' + i)) }
