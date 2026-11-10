// Package dayview is the day view of the TUI: one day of the archive in
// time order, opened from the activity view (a click on a day, enter on
// the selected one). The head has the numbers of stats --workday --blocks
// (first and last activity, span, work, breaks, target, balance) and the
// estimate of stats --worktime; the body lists the activity blocks of the
// day with a bar in project colours, the projects of each block with their
// time and the sessions in it, the pauses between the blocks, and then the
// projects of the day with their time and their events split by human and
// agent.
//
// The view calculates nothing: package dayreport assembles the day from
// the store functions behind stats --workday --blocks, --worktime and
// timeline, and `total-recall stats --day` prints the same report. A
// block or a session is selected with the cursor; enter (or a click)
// opens the session at the first event of the block or segment. The keys
// for the previous and the next day move through the days (not past
// today); esc returns to the activity view.
//
// Colours come from the theme and the shared chart.Colors, so a project
// has the colour it has in the stats and activity views; every coloured
// element also differs in glyph, so NO_COLOR stays legible.
package dayview

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/dayreport"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// Open asks the app to show the view for a day (any time of it); the
// activity view sends it. HumanOnly is the counting mode of the activity
// view.
type Open struct {
	Day       time.Time
	HumanOnly bool
}

// Back is sent when the user leaves the view; Day is the day it showed,
// so the activity view can select it.
type Back struct{ Day time.Time }

// OpenSession asks the app to show a session at an event.
type OpenSession struct{ SessionID, EventID string }

// Options configure the view.
type Options struct {
	// Source loads the day; nil means no database, the view shows a hint.
	Source dayreport.Source
	// Report are the rules the day is counted under (the same as of
	// stats --workday and --worktime); its HumanOnly is the initial
	// counting mode, an Open overrides it. Location defaults to UTC.
	Report dayreport.Options
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
	// Colors maps projects to colours, shared with the other views; nil
	// means own colours.
	Colors *chart.Colors
}

func (o *Options) defaults() {
	if o.Report.Location == nil {
		o.Report.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Colors == nil {
		o.Colors = &chart.Colors{}
	}
}

// resultMsg is the answer of one load.
type resultMsg struct {
	seq uint64
	day time.Time
	rep dayreport.Report
	err error
}

// Model is the day view.
type Model struct {
	opts Options
	keys KeyMap
	th   theme.Theme

	day       time.Time // local midnight of the day shown
	humanOnly bool
	seq       uint64 // numbers the loads; an older answer is dropped
	rep       *dayreport.Report
	err       error
	loaded    bool // a load was started for day

	cursor, offset int
	width, height  int
}

// New returns the view for opts.
func New(opts Options) Model {
	opts.defaults()
	return Model{opts: opts, keys: newKeyMap(opts.Keys), th: theme.New(true), humanOnly: opts.Report.HumanOnly}
}

// SetKeys swaps the key bindings.
func (m Model) SetKeys(km keys.Map) tea.Model {
	m.opts.Keys, m.keys = km, newKeyMap(km)
	return m
}

// SetOptions swaps the options (settings changed in the config view) and
// loads the day again when one is shown. The counting mode stays.
func (m Model) SetOptions(opts Options) (tea.Model, tea.Cmd) {
	opts.defaults()
	m.opts = opts
	if !m.loaded {
		return m, nil
	}
	return m.fetch()
}

// Title implements tui.View.
func (Model) Title() string { return "day" }

// Init implements tea.Model.
func (Model) Init() tea.Cmd { return nil }

// ShortHelp and FullHelp implement help.KeyMap.
func (m Model) ShortHelp() []key.Binding  { return m.keys.ShortHelp() }
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// Reload loads the day on screen again, keeping the cursor.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	if !m.loaded {
		return m, nil
	}
	return m.fetch()
}

// Day is the day shown, as a local midnight; zero before the first Open.
func (m Model) Day() time.Time { return m.day }

func (m Model) loc() *time.Location { return m.opts.Report.Location }

// today is the day of now under the day rule.
func (m Model) today() time.Time {
	return store.DayOf(m.opts.Now(), m.loc(), m.opts.Report.DayStart)
}

// problem is why the view cannot load a day, "" when it can.
func (m Model) problem() string {
	switch {
	case m.opts.Source == nil:
		return "no database"
	case m.opts.Report.BlockGap <= 0 || m.opts.Report.Worktime.MaxGap <= 0:
		return "the day view needs a valid [work] block_gap and [worktime] table (see the config view)"
	}
	return ""
}

// fetch starts the load of the current day and counting mode.
func (m Model) fetch() (Model, tea.Cmd) {
	m.seq++
	if m.problem() != "" {
		return m, nil
	}
	seq, day, o := m.seq, m.day, m.opts.Report
	o.HumanOnly = m.humanOnly
	src := m.opts.Source
	return m, func() tea.Msg {
		rep, err := dayreport.Load(context.Background(), src, day, o)
		return resultMsg{seq: seq, day: day, rep: rep, err: err}
	}
}

// show switches to a day: the report is cleared until the answer arrives.
func (m Model) show(day time.Time) (Model, tea.Cmd) {
	y, mo, d := day.In(m.loc()).Date()
	m.day = time.Date(y, mo, d, 0, 0, 0, 0, m.loc())
	m.rep, m.err, m.loaded = nil, nil, true
	m.cursor, m.offset = 0, 0
	return m.fetch()
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case Open:
		m.humanOnly = msg.HumanOnly
		return m.show(msg.Day)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollTo()
		return m, nil
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
		return m, nil
	case resultMsg:
		if msg.seq != m.seq || !msg.day.Equal(m.day) {
			return m, nil
		}
		m.err = msg.err
		if msg.err == nil {
			m.rep = &msg.rep
		}
		m.clamp()
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseClickMsg:
		return m.click(msg)
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.move(-1)
		case tea.MouseWheelDown:
			m.move(1)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Back):
		return m, func() tea.Msg { return Back{Day: m.day} }
	case key.Matches(msg, k.Prev):
		return m.step(-1)
	case key.Matches(msg, k.Next):
		return m.step(1)
	case key.Matches(msg, k.Up):
		m.move(-1)
	case key.Matches(msg, k.Down):
		m.move(1)
	case key.Matches(msg, k.Open):
		return m.openSelected()
	case key.Matches(msg, k.Human):
		m.humanOnly = !m.humanOnly
		m.rep, m.err = nil, nil
		return m.fetch()
	}
	return m, nil
}

// step moves by n days; the view does not go past today.
func (m Model) step(n int) (tea.Model, tea.Cmd) {
	next := m.day.AddDate(0, 0, n)
	if next.After(m.today()) {
		return m, nil
	}
	return m.show(next)
}

// item is a selectable row: a block or a session segment of one.
type item struct {
	block, session int // session -1 for the block row
}

// items lists the selectable rows in order.
func (m Model) items() []item {
	if m.rep == nil {
		return nil
	}
	var out []item
	for i, b := range m.rep.Blocks {
		out = append(out, item{i, -1})
		for j := range b.Sessions {
			out = append(out, item{i, j})
		}
	}
	return out
}

// move moves the cursor by n rows; at the first or last selectable row it
// scrolls the body instead, so the projects below the last block can be
// reached.
func (m *Model) move(n int) {
	old := m.cursor
	m.cursor += n
	m.clamp()
	if m.cursor != old {
		return
	}
	l := m.layout()
	m.offset = max(0, min(m.offset+n, len(l.rows)-max(m.height-l.bodyTop, 0)))
}

func (m *Model) clamp() {
	m.cursor = max(0, min(m.cursor, len(m.items())-1))
	m.scrollTo()
}

// openSelected opens the session of the selected row: the first one of a
// block, at its first event.
func (m Model) openSelected() (tea.Model, tea.Cmd) {
	its := m.items()
	if m.cursor < 0 || m.cursor >= len(its) {
		return m, nil
	}
	it := its[m.cursor]
	b := m.rep.Blocks[it.block]
	if len(b.Sessions) == 0 {
		return m, nil
	}
	s := b.Sessions[max(it.session, 0)]
	return m, func() tea.Msg { return OpenSession{SessionID: s.SessionID, EventID: s.FirstEvent} }
}

// click handles a left click: on the arrows of the title the previous or
// next day, on a block or session row selects and opens it.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.opts.Source == nil {
		return m, nil
	}
	if msg.Y == 0 {
		_, prev, next := m.title()
		switch {
		case prev.Contains(msg.X):
			return m.step(-1)
		case next.Contains(msg.X):
			return m.step(1)
		}
		return m, nil
	}
	l := m.layout()
	row := msg.Y - l.bodyTop + m.offset
	if row >= 0 && row < len(l.rows) && l.rows[row].item >= 0 {
		m.cursor = l.rows[row].item
		return m.openSelected()
	}
	return m, nil
}

// ---- rendering ----

// row is one line of the scrollable body; item is its index in items(),
// -1 for a row that cannot be selected.
type row struct {
	text string
	item int
}

type layoutT struct {
	head    []string
	rows    []row
	bodyTop int
}

// glyphs of the projects by rank in the day's list, then the rest.
var glyphs = [...]rune{'█', '▓', '▒', '▞', '▚', '▌'}

const otherGlyph = '▪'

// title is the first line, with the columns of its two arrows.
func (m Model) title() (string, zone.Span, zone.Span) {
	d := m.day
	date := d.Format("Mon 2006-01-02")
	left, right := "‹", "›"
	if !d.Before(m.today()) {
		right = " "
	}
	line := " " + left + " " + m.th.Title.Render(date) + " " + right
	prev := zone.Span{Start: 1, End: 2}
	next := zone.Span{Start: 4 + lipgloss.Width(date), End: 5 + lipgloss.Width(date)}
	if right == " " {
		next = zone.Span{}
	}
	if m.rep != nil {
		switch w := m.rep.Workday; {
		case w.IsHoliday():
			line += "  " + m.th.HolidayText.Render(w.Holiday)
		case store.IsWeekend(w.Day):
			line += "  " + m.th.WeekendText.Render("weekend")
		}
	}
	mode := "all events"
	if m.humanOnly {
		mode = "human events only"
	}
	line += "  " + m.th.Muted.Render("· "+mode+" ["+keys.Display(m.opts.Keys.First(keys.DayHuman))+"]")
	return line, prev, next
}

// projectStyle is the glyph and style of a project key.
type projectStyle struct {
	glyph rune
	style lipgloss.Style
}

func (m Model) projectStyles() map[string]projectStyle {
	out := map[string]projectStyle{}
	if m.rep == nil {
		return out
	}
	for i, p := range m.rep.Projects {
		ps := projectStyle{glyph: otherGlyph, style: m.th.Muted}
		if i < len(glyphs) {
			ps = projectStyle{glyphs[i], lipgloss.NewStyle().Foreground(m.th.Tone(m.opts.Colors.Color(p.Key)))}
		}
		out[p.Key] = ps
	}
	return out
}

func (m Model) layout() layoutT {
	var l layoutT
	l.head = []string{}
	t, _, _ := m.title()
	l.head = append(l.head, t)
	if p := m.problem(); p != "" {
		l.head = append(l.head, "", " "+m.th.Muted.Render(p))
		l.bodyTop = len(l.head)
		return l
	}
	if m.err != nil {
		l.head = append(l.head, "", " "+m.th.Error.Render("error: "+m.err.Error()))
		l.bodyTop = len(l.head)
		return l
	}
	if m.rep == nil {
		l.head = append(l.head, "", " "+m.th.Muted.Render("loading…"))
		l.bodyTop = len(l.head)
		return l
	}
	r, st := m.rep, m.th
	w := r.Workday
	if !w.Active() {
		l.head = append(l.head, "", " "+st.Muted.Render("no activity on this day"))
		l.bodyTop = len(l.head)
		return l
	}
	loc := m.loc()
	rate := ratingStyle(st, r.Rating)
	h1 := fmt.Sprintf(" %s %s · %s %s · %s %s · %s %s · %s %s",
		st.Muted.Render("first"), w.FirstClock(loc), st.Muted.Render("last"), w.LastClock(loc),
		st.Muted.Render("span"), rate.Render(store.FormatSpan(w.Span())),
		st.Muted.Render("work"), store.FormatSpan(w.BlockSum()), st.Muted.Render("breaks"), store.FormatSpan(w.BreakSum()))
	h2 := " "
	if r.HasDiff {
		h2 += fmt.Sprintf("%s %s · %s %s · ", st.Muted.Render("target"), store.FormatSpan(r.Target), st.Muted.Render("diff"), rate.Render(store.FormatDiff(r.Diff)))
	}
	h2 += fmt.Sprintf("%s %s (%s %s)", st.Muted.Render("working time"), store.FormatSpan(r.Worktime.Raw), st.Muted.Render("rounded"), store.FormatSpan(r.Worktime.Rounded))
	h3 := fmt.Sprintf(" %s %d (%s %d · %s %d) · %s %d",
		st.Muted.Render("events"), w.Human+w.Agent, st.Muted.Render("human"), w.Human, st.Muted.Render("agent"), w.Agent, st.Muted.Render("sessions"), w.Sessions)
	l.head = append(l.head, h1, h2, h3, "")
	l.bodyTop = len(l.head)
	l.rows = m.bodyRows(m.projectStyles())
	return l
}

func ratingStyle(th theme.Theme, r store.Rating) lipgloss.Style {
	switch r {
	case store.RatingUnder:
		return lipgloss.NewStyle().Foreground(th.Under)
	case store.RatingOn:
		return lipgloss.NewStyle().Foreground(th.On)
	case store.RatingOver:
		return lipgloss.NewStyle().Foreground(th.Over)
	}
	return lipgloss.NewStyle()
}

// bodyRows renders the blocks with their sessions and pauses, then the
// projects of the day.
func (m Model) bodyRows(ps map[string]projectStyle) []row {
	r, st, loc := m.rep, m.th, m.loc()
	w := r.Workday
	wide := m.width >= 90
	barW := 0
	if wide {
		barW = 24
	}
	var rows []row
	idx := 0
	for i, b := range r.Blocks {
		if b.Pause > 0 {
			rows = append(rows, row{text: st.Muted.Render("      ··· pause " + store.FormatSpan(b.Pause)), item: -1})
		}
		span := w.Clock(b.Start, loc) + "–" + w.Clock(b.End, loc)
		if b.Single() {
			span = w.Clock(b.Start, loc) + "*"
		}
		var parts []string
		for _, p := range b.Projects {
			parts = append(parts, p.Name+" "+store.FormatSpan(time.Duration(p.Secs)*time.Second))
		}
		line := fmt.Sprintf(" %s %2d %-13s %6s  %4d ev (h%d a%d)", " ", i+1, span, store.FormatSpan(time.Duration(b.Secs)*time.Second), b.Events, b.Human, b.Agent)
		if barW > 0 {
			line += "  " + blockBar(b, ps, barW)
		}
		line += "  " + strings.Join(parts, " · ")
		rows = append(rows, row{text: line, item: idx})
		idx++
		for _, s := range b.Sessions {
			title := s.Title
			if title == "" {
				title = s.SessionID
			}
			p := ps[s.Project]
			line := fmt.Sprintf("       %s–%s  %s %s", w.Clock(s.Start, loc), w.Clock(s.End, loc), p.style.Render(string(p.glyph)), title)
			line += "  " + st.Muted.Render(fmt.Sprintf("h%d a%d", s.Human, s.Agent))
			rows = append(rows, row{text: line, item: idx})
			idx++
		}
	}
	rows = append(rows, row{item: -1}, row{text: " " + st.Label.Render("projects"), item: -1})
	nameW := 7
	for _, p := range r.Projects {
		nameW = max(nameW, lipgloss.Width(p.Name))
	}
	rows = append(rows, row{text: st.Muted.Render(fmt.Sprintf("   %s  %7s  %7s  %6s  %6s", pad("", nameW+2), "time", "rounded", "human", "agent")), item: -1})
	for _, p := range r.Projects {
		s := ps[p.Key]
		rows = append(rows, row{text: fmt.Sprintf("   %s %s  %7s  %7s  %6d  %6d", s.style.Render(string(s.glyph)), pad(p.Name, nameW), store.FormatSpan(p.Raw), store.FormatSpan(p.Rounded), p.Human, p.Agent), item: -1})
	}
	rows = append(rows, row{text: fmt.Sprintf("   %s %s  %7s  %7s  %6d  %6d", " ", pad("total", nameW), store.FormatSpan(r.Worktime.Raw), store.FormatSpan(r.Worktime.Rounded), w.Human, w.Agent), item: -1})
	return rows
}

// blockBar draws a block as a bar of n cells in the glyphs and colours of
// its projects, each taking cells by its share of the block's seconds; a
// block without seconds (a single event) is one cell of its project.
func blockBar(b dayreport.Block, ps map[string]projectStyle, n int) string {
	if len(b.Projects) == 0 {
		return strings.Repeat(" ", n)
	}
	total := 0
	for _, p := range b.Projects {
		total += p.Secs
	}
	var sb strings.Builder
	used, acc := 0, 0
	for _, p := range b.Projects {
		var cells int
		if total == 0 {
			if used == 0 {
				cells = 1
			}
		} else {
			acc += p.Secs
			cells = acc*n/total - used
		}
		cells = max(cells, 0)
		s := ps[p.Key]
		sb.WriteString(s.style.Render(strings.Repeat(string(s.glyph), cells)))
		used += cells
	}
	return sb.String() + strings.Repeat(" ", max(n-used, 0))
}

func pad(s string, n int) string { return s + strings.Repeat(" ", max(n-lipgloss.Width(s), 0)) }

// scrollTo keeps the selected row in view.
func (m *Model) scrollTo() {
	l := m.layout()
	h := max(m.height-l.bodyTop, 0)
	if h == 0 {
		return
	}
	line := -1
	for i, r := range l.rows {
		if r.item == m.cursor {
			line = i
		}
	}
	switch {
	case line < 0:
		m.offset = 0
	case line < m.offset:
		m.offset = line
	case line >= m.offset+h:
		m.offset = line - h + 1
	}
	m.offset = max(0, min(m.offset, max(len(l.rows)-h, 0)))
}

// View implements tea.Model.
func (m Model) View() tea.View {
	l := m.layout()
	lines := append([]string{}, l.head...)
	h := max(m.height-l.bodyTop, 0)
	for i := 0; i < h; i++ {
		ri := m.offset + i
		if ri >= len(l.rows) {
			break
		}
		r := l.rows[ri]
		text := r.text
		if r.item == m.cursor && r.item >= 0 {
			text = m.th.Selected.Render(pad(ansi.Strip("▸"+strings.TrimPrefix(text, " ")), max(m.width, 0)))
		}
		lines = append(lines, ansi.Truncate(text, max(m.width, 1), "…"))
	}
	for i, ln := range lines {
		lines[i] = ansi.Truncate(ln, max(m.width, 1), "…")
	}
	return tea.NewView(strings.Join(lines, "\n"))
}
