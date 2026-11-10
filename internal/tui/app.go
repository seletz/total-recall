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

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/activity"
	"github.com/seletz/total-recall/internal/tui/attview"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/configview"
	"github.com/seletz/total-recall/internal/tui/dayview"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/memview"
	"github.com/seletz/total-recall/internal/tui/search"
	"github.com/seletz/total-recall/internal/tui/session"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/zone"
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
	// memory is the memory view, a layer like the detail: shown while
	// showMemory is set.
	memory     memview.Model
	showMemory bool
	// attach is the attachment view, a layer like the detail. It is
	// opened from a search hit or from the detail; attachFromDetail makes
	// Back return to the detail then.
	attach           attview.Model
	showAttach       bool
	attachFromDetail bool
	// day is the day view, a layer like the detail, opened from the
	// activity view. A session opened from it is shown in the detail
	// (detailFromDay), and Back returns to the day.
	day           dayview.Model
	showDay       bool
	detailFromDay bool
	colors        *chart.Colors
	keys          KeyMap
	help          bool
	width         int
	height        int
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
	// busy is set while an action of the config view writes to the
	// archive: the checks pause, so a maintenance run is not disturbed by
	// a reader, and everything reloads when the action is done.
	busy bool
}

// Init starts every view, asks the terminal for its background colour (the
// answer reaches every view through Update) and starts the reload cycle.
func (a App) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(a.views)+4)
	for _, v := range a.views {
		cmds = append(cmds, v.Init())
	}
	return tea.Batch(append(cmds, a.detail.Init(), a.memory.Init(), a.attach.Init(), a.day.Init(), tea.RequestBackgroundColor, a.tick())...)
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
	a.stale = make([]bool, len(a.views)+4)
	for i := range a.stale {
		a.stale[i] = true
	}
	a.updated = a.opts.Now()
	return a.reloadCurrent()
}

// reloadCurrent reloads the view on screen (the detail while it is shown)
// and clears its stale mark.
func (a *App) reloadCurrent() tea.Cmd {
	if a.showAttach {
		a.setStale(len(a.views)+2, false)
		m, cmd := a.attach.Reload()
		a.attach = m.(attview.Model)
		return cmd
	}
	if a.showMemory {
		a.setStale(len(a.views)+1, false)
		m, cmd := a.memory.Reload()
		a.memory = m.(memview.Model)
		return cmd
	}
	if a.showDetail {
		a.setStale(len(a.views), false)
		m, cmd := a.detail.Reload()
		a.detail = m.(session.Model)
		return cmd
	}
	if a.showDay {
		a.setStale(len(a.views)+3, false)
		m, cmd := a.day.Reload()
		a.day = m.(dayview.Model)
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
	// An image of the attachment view goes out of the terminal with it.
	var leave tea.Cmd
	if a.showAttach {
		leave = a.attach.Leave()
	}
	a.active, a.showDetail, a.showMemory, a.showAttach, a.showDay, a.detailFromDay = i, false, false, false, false, false
	if !a.stale[i] {
		return leave
	}
	return tea.Batch(leave, a.reloadCurrent())
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
	case dayview.Open:
		a.showDay, a.showDetail, a.detailFromDay = true, false, false
		a.setStale(len(a.views)+3, false) // Open loads afresh
		return a.updateDay(msg)
	case dayview.OpenSession:
		a.showDay, a.showDetail, a.detailFromDay = false, true, true
		a.setStale(len(a.views), false) // Open loads afresh
		return a.updateDetail(session.Open{SessionID: msg.SessionID, EventID: msg.EventID})
	case dayview.Back:
		a.showDay = false
		cmd := a.show(a.active)
		m, c := a.views[a.active].Update(activity.Select{Day: msg.Day})
		a.views = a.replace(a.active, m)
		return a, tea.Batch(cmd, c)
	case search.OpenSession:
		a.detailFromDay = false
		a.showDetail = true
		a.setStale(len(a.views), false) // Open loads afresh
		return a.updateDetail(session.Open{SessionID: msg.SessionID, EventID: msg.EventID, Terms: msg.Terms})
	case memview.Open:
		a.showMemory, a.showDetail = true, false
		a.setStale(len(a.views)+1, false) // Open loads afresh
		return a.updateMemory(msg)
	case session.OpenAttachments:
		return a.openAttachments(attview.Open(msg), true)
	case attview.Open:
		return a.openAttachments(msg, a.showDetail)
	case attview.Back:
		a.showAttach = false
		if a.attachFromDetail {
			a.showDetail = true
			if a.stale[len(a.views)] {
				return a, a.reloadCurrent() // the detail went stale underneath
			}
			return a, nil
		}
		return a, a.show(a.active)
	case session.Back:
		if a.detailFromDay {
			a.detailFromDay, a.showDetail, a.showDay = false, false, true
			if a.stale[len(a.views)+3] {
				return a, a.reloadCurrent() // the day went stale underneath
			}
			return a, nil
		}
		return a, a.show(a.active)
	case memview.Back:
		return a, a.show(a.active)
	case tickMsg:
		if a.opts.Store == nil {
			return a, nil
		}
		if a.busy {
			return a, a.tick()
		}
		return a, a.poll()
	case configview.Busy:
		return a.setBusy(msg.On)
	case configview.Changed:
		return a.configChanged(msg)
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
	case key.Matches(msg, a.keys.Config):
		return a, a.show(3)
	case key.Matches(msg, a.keys.Reload):
		if a.opts.Store == nil {
			return a, nil
		}
		a.reloadErr = nil
		return a, a.reload()
	}
	return a.forward(msg)
}

// setBusy pauses the reload while an action writes and reloads everything
// once it is done; the view gets the message too.
func (a App) setBusy(on bool) (tea.Model, tea.Cmd) {
	a.busy = on
	_, cmd := a.broadcastModel(configview.Busy{On: on})
	if on || a.opts.Store == nil {
		return a, cmd
	}
	// The file may have been swapped (maintenance): let the next check
	// decide between a reload and a reopen, right away.
	return a, tea.Batch(cmd, a.poll())
}

// configChanged applies what the running TUI takes from the configuration
// (see configview.Changed): the reload interval (a reload that was off
// starts), the mouse, the project definitions, the working time and the
// start modes of the activity view, and the key bindings. A table with a
// problem keeps the values the app started with.
func (a App) configChanged(msg configview.Changed) (tea.Model, tea.Cmd) {
	cfg := msg.Config
	wasOff := a.opts.Reload <= 0
	a.opts.Reload = cfg.TUI.Reload.Duration
	a.opts.NoMouse = !cfg.TUI.Mouse
	_, cmd := a.broadcastModel(msg)
	cmds := []tea.Cmd{cmd, a.setProjects(cfg), a.setWork(cfg), a.setDay(cfg)}
	a.setKeys(cfg)
	if wasOff && a.opts.Reload > 0 {
		cmds = append(cmds, a.tick())
	}
	return a, tea.Batch(cmds...)
}

// projectsView is a view that applies the project definitions itself:
// stats and activity swap their resolver and reload.
type projectsView interface {
	SetProjects(project.Resolver) (tea.Model, tea.Cmd)
}

// setProjects hands changed project definitions to the views that use
// them, so a definition edited in the config view shows at once. A list
// with a problem keeps the definitions the app started with.
func (a *App) setProjects(cfg *config.Config) tea.Cmd {
	r, err := cfg.ProjectResolver()
	if err != nil || r.Equal(a.opts.Projects) {
		return nil
	}
	a.opts.Projects = r
	var cmds []tea.Cmd
	for i, v := range a.views {
		pv, ok := v.(projectsView)
		if !ok {
			continue
		}
		m, cmd := pv.SetProjects(r)
		a.views = a.replace(i, m.(View))
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// workView is the activity view: it applies the day boundary, the working
// time, the holidays and its start modes itself. holidaysView is the
// stats view, which marks the holidays on its day axis.
type (
	workView interface {
		SetSettings(activity.Settings) (tea.Model, tea.Cmd)
	}
	holidaysView interface {
		SetHolidays(store.Holidays) (tea.Model, tea.Cmd)
	}
)

// setWork hands day_start, [work], tui.activity_projects and
// tui.activity_newest to the views that use them; a view decides what a change needs (a redraw, a reload or
// a mode switch) and ignores what did not change. A [work] table with a
// problem keeps the values the app started with.
func (a *App) setWork(cfg *config.Config) tea.Cmd {
	work, err := ops.WorkHours(cfg)
	if err != nil {
		return nil
	}
	s := activity.Settings{DayStart: cfg.DayStartOffset(), Work: work, Holidays: ops.Holidays(cfg), HumanOnly: cfg.Work.HumanOnly(), ByProject: cfg.TUI.ActivityProjects, Newest: cfg.TUI.ActivityNewest}
	a.opts.DayStart, a.opts.Work, a.opts.Holidays, a.opts.HumanOnly, a.opts.ActivityProjects, a.opts.ActivityNewest = s.DayStart, s.Work, s.Holidays, s.HumanOnly, s.ByProject, s.Newest
	var cmds []tea.Cmd
	for i, v := range a.views {
		var m tea.Model
		var cmd tea.Cmd
		switch v := v.(type) {
		case workView:
			m, cmd = v.SetSettings(s)
		case holidaysView:
			m, cmd = v.SetHolidays(s.Holidays)
		default:
			continue
		}
		a.views = a.replace(i, m)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// setDay hands the working-time settings, the block gap and what setWork
// and setProjects took over to the day view, which loads its day again.
// A table with a problem keeps the values the app started with.
func (a *App) setDay(cfg *config.Config) tea.Cmd {
	if s, err := cfg.WorktimeSettings(); err == nil {
		a.opts.Worktime = s
	}
	if g := cfg.Work.BlockGap.Duration; g > 0 {
		a.opts.BlockGap = g
	}
	m, cmd := a.day.SetOptions(dayOptions(a.opts, a.colors))
	a.day = m.(dayview.Model)
	return cmd
}

// keysView is a view that takes over changed key bindings itself.
type keysView interface {
	SetKeys(keys.Map) tea.Model
}

// setKeys hands a changed [keys] table to the app shell, every view and
// the detail, so the footer, the help and the keys themselves follow at
// once. A table with a problem keeps the bindings in effect.
func (a *App) setKeys(cfg *config.Config) {
	km, err := cfg.KeyMap()
	if err != nil || km.Equal(a.opts.Keys) {
		return
	}
	a.opts.Keys = km
	a.keys = newKeyMap(km)
	for i, v := range a.views {
		if kv, ok := v.(keysView); ok {
			a.views = a.replace(i, kv.SetKeys(km))
		}
	}
	a.detail = a.detail.SetKeys(km).(session.Model)
	a.memory = a.memory.SetKeys(km).(memview.Model)
	a.attach = a.attach.SetKeys(km).(attview.Model)
	a.day = a.day.SetKeys(km).(dayview.Model)
}

// openAttachments shows the attachment view above the view it was opened
// from: the detail (fromDetail, which Back returns to) or the search.
func (a App) openAttachments(o attview.Open, fromDetail bool) (tea.Model, tea.Cmd) {
	a.showAttach, a.attachFromDetail = true, fromDetail
	a.showDetail, a.showMemory, a.showDay = false, false, false
	a.setStale(len(a.views)+2, false) // Open loads afresh
	return a.updateAttach(o)
}

// broadcastModel is broadcast on a value receiver.
func (a *App) broadcastModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.broadcast(msg)
	return a, cmd
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
	if a.showAttach {
		return a.attach
	}
	if a.showMemory {
		return a.memory
	}
	if a.showDetail {
		return a.detail
	}
	if a.showDay {
		return a.day
	}
	return a.views[a.active]
}

// forward passes msg to the current view only.
func (a App) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.showAttach {
		return a.updateAttach(msg)
	}
	if a.showMemory {
		return a.updateMemory(msg)
	}
	if a.showDetail {
		return a.updateDetail(msg)
	}
	if a.showDay {
		return a.updateDay(msg)
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

// updateDay passes msg to the day view.
func (a App) updateDay(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.day.Update(msg)
	a.day = m.(dayview.Model)
	return a, cmd
}

// updateMemory passes msg to the memory view.
func (a App) updateMemory(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.memory.Update(msg)
	a.memory = m.(memview.Model)
	return a, cmd
}

// updateAttach passes msg to the attachment view.
func (a App) updateAttach(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.attach.Update(msg)
	a.attach = m.(attview.Model)
	return a, cmd
}

// broadcast passes msg to every view, the detail and the layers.
func (a *App) broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(a.views)+4)
	for i, v := range a.views {
		m, cmd := v.Update(msg)
		a.views = a.replace(i, m)
		cmds[i] = cmd
	}
	m, cmd := a.detail.Update(msg)
	a.detail, cmds[len(a.views)] = m.(session.Model), cmd
	m, cmd = a.memory.Update(msg)
	a.memory, cmds[len(a.views)+1] = m.(memview.Model), cmd
	m, cmd = a.attach.Update(msg)
	a.attach, cmds[len(a.views)+2] = m.(attview.Model), cmd
	m, cmd = a.day.Update(msg)
	a.day, cmds[len(a.views)+3] = m.(dayview.Model), cmd
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
// the time of the last load (muted; or the reload error) in the middle
// and the view tabs on the right; the active tab is marked with brackets
// on the accent background, so it reads without colour too. While the
// detail is shown its title follows the active tab. The time gives way to
// the tabs in a narrow window.
func (a App) header() string {
	left := " " + a.th.Gradient("total-recall")
	right := strings.Join(a.tabs(), " ") + " "
	gap := max(a.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	status, important := a.reloadStatus()
	if status != "" && lipgloss.Width(status)+2 > gap {
		// The time gives way to the tabs; an error or a pending migration
		// is cut instead, as long as a few cells remain.
		status = ""
		if important && gap >= 10 {
			status = a.th.Error.Render(ansi.Truncate(a.statusText(), gap-2, "…"))
		}
	}
	if status != "" {
		pad := (gap - lipgloss.Width(status)) / 2
		return left + strings.Repeat(" ", pad) + status + strings.Repeat(" ", gap-pad-lipgloss.Width(status)) + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// reloadStatus is the middle of the header: "updated 14:03:12" in the
// time zone of the views, or the last reload error or a pending migration
// (important: shown even in a narrow window); empty without a store.
func (a App) reloadStatus() (status string, important bool) {
	if a.opts.Store == nil {
		return "", false
	}
	if t := a.statusText(); t != "" {
		return a.th.Error.Render(ansi.Truncate(t, max(a.width/2, 1), "…")), true
	}
	return a.th.Muted.Render("updated " + a.updated.In(a.opts.Location).Format("15:04:05")), false
}

// statusText is the important part of reloadStatus, plain: the last
// reload error or the pending migration; "" when all is well.
func (a App) statusText() string {
	switch {
	case a.reloadErr != nil:
		return "reload: " + a.reloadErr.Error()
	case a.opts.Store.MigrationPending():
		return "migration pending: total-recall migrate"
	}
	return ""
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
		switch {
		case a.showAttach && a.attachFromDetail:
			t += a.th.AccentText.Render(" › " + a.detail.Title() + " › " + a.attach.Title())
		case a.showAttach:
			t += a.th.AccentText.Render(" › " + a.attach.Title())
		case a.showMemory:
			t += a.th.AccentText.Render(" › " + a.memory.Title())
		case a.showDetail && a.detailFromDay:
			t += a.th.AccentText.Render(" › " + a.day.Title() + " › " + a.detail.Title())
		case a.showDetail:
			t += a.th.AccentText.Render(" › " + a.detail.Title())
		case a.showDay:
			t += a.th.AccentText.Render(" › " + a.day.Title())
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
