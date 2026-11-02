// Package activity is the first/last-activity view of the TUI: per day of
// the window (one day, one ISO week or one month) a bar from the first to
// the last activity on a time axis that starts at the configured day
// boundary, the clock times and the span as text, and for a week or a
// month a total line.
//
// Every change of window or counting mode loads a store.Workday as a
// command; results carry the sequence number of the change they belong
// to, so a slow answer to an older change is dropped. Loaded days are
// cached per filter, so switching back is instant. The chart is text only:
// a row per day is a table as much as a chart, so there is no image path.
package activity

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/window"
)

// MinBarWidth is the narrowest time axis drawn; below it the rows show the
// times only.
const MinBarWidth = 12

// Loader loads the days; *store.Store implements it.
type Loader interface {
	Workday(ctx context.Context, f store.WorkdayFilter) ([]store.Workday, error)
}

// Options configure the view.
type Options struct {
	// Loader answers the queries; nil means no database, the view shows a hint.
	Loader Loader
	// Location is the time zone of the days; nil means UTC.
	Location *time.Location
	// Now is the clock the initial window contains; nil means time.Now.
	Now func() time.Time
	// DayStart is the wall-clock time at which a day begins, as an offset
	// from midnight (4h = 04:00); zero means midnight.
	DayStart time.Duration
}

func (o *Options) defaults() {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

// filter is one cache key: the parameters of a load.
type filter struct {
	since, until int64 // unix seconds
	humanOnly    bool
}

// resultMsg is the answer of one load; it is shown only when seq is still
// the current sequence, and cached in any case.
type resultMsg struct {
	seq    uint64
	filter filter
	days   []store.Workday
	took   time.Duration
	err    error
}

// Model is the activity view.
type Model struct {
	opts      Options
	keys      KeyMap
	window    window.Window // always a single period
	humanOnly bool

	seq    uint64
	cancel context.CancelFunc

	cache   map[filter][]store.Workday
	loaded  bool // days holds the data of the current filter
	loading bool
	days    []store.Workday
	took    time.Duration
	err     error

	offset        int // first visible day row
	initCmd       tea.Cmd
	width, height int
}

// New returns the view for opts: the week containing now, human and agent
// events.
func New(opts Options) Model {
	opts.defaults()
	m := Model{
		opts:   opts,
		keys:   defaultKeyMap(),
		window: window.Single(store.PeriodWeek, opts.Now(), opts.Location),
		cache:  map[filter][]store.Workday{},
	}
	m, m.initCmd = m.load()
	return m
}

// Title implements tui.View.
func (Model) Title() string { return "activity" }

// Init implements tea.Model: it starts the first load.
func (m Model) Init() tea.Cmd { return m.initCmd }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampOffset()
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case resultMsg:
		return m.result(msg), nil
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.opts.Loader == nil {
		return m, nil
	}
	k := m.keys
	switch {
	case key.Matches(msg, k.Day):
		return m.setWindow(m.window.WithSinglePeriod(store.PeriodDay))
	case key.Matches(msg, k.Week):
		return m.setWindow(m.window.WithSinglePeriod(store.PeriodWeek))
	case key.Matches(msg, k.Month):
		return m.setWindow(m.window.WithSinglePeriod(store.PeriodMonth))
	case key.Matches(msg, k.Earlier):
		return m.setWindow(m.window.Shift(-1))
	case key.Matches(msg, k.Later):
		return m.setWindow(m.window.Shift(1))
	case key.Matches(msg, k.Current):
		return m.setWindow(m.current())
	case key.Matches(msg, k.Last):
		return m.setWindow(m.current().Shift(-1))
	case key.Matches(msg, k.Human):
		m.humanOnly = !m.humanOnly
		return m.load()
	case key.Matches(msg, k.Up):
		m.offset--
		m.clampOffset()
	case key.Matches(msg, k.Down):
		m.offset++
		m.clampOffset()
	}
	return m, nil
}

// current is the window of the current period containing now.
func (m Model) current() window.Window {
	return window.Single(m.window.Period, m.opts.Now(), m.opts.Location)
}

func (m Model) setWindow(w window.Window) (tea.Model, tea.Cmd) {
	if w.Equal(m.window) {
		return m, nil
	}
	m.window, m.offset = w, 0
	return m.load()
}

// since and until are the bounds of the window under the day rule: the
// day boundary of its first day and of the day after its last.
func (m Model) since() time.Time { return store.DayBoundary(m.window.Since(), m.opts.DayStart) }
func (m Model) until() time.Time { return store.DayBoundary(m.window.Until(), m.opts.DayStart) }

// filter is the cache key of the current state.
func (m Model) filter() filter {
	return filter{since: m.since().Unix(), until: m.until().Unix(), humanOnly: m.humanOnly}
}

// load shows the cached days of the current filter, or starts loading them.
// A running load is cancelled; its result would be stale.
func (m Model) load() (Model, tea.Cmd) {
	m.seq++
	m.stop()
	m.err = nil
	if m.opts.Loader == nil {
		return m, nil
	}
	f := m.filter()
	if d, ok := m.cache[f]; ok {
		m.days, m.loaded, m.took = d, true, 0
		return m, nil
	}
	m.loaded, m.loading = false, true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	seq, now, l := m.seq, m.opts.Now, m.opts.Loader
	wf := store.WorkdayFilter{
		Since: m.since(), Until: m.until(), Location: m.opts.Location,
		DayStart: m.opts.DayStart, HumanOnly: m.humanOnly,
	}
	return m, func() tea.Msg {
		start := now()
		d, err := l.Workday(ctx, wf)
		return resultMsg{seq: seq, filter: f, days: d, took: now().Sub(start), err: err}
	}
}

// stop cancels the running load, if any.
func (m *Model) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.loading = false
}

// result caches a successful answer and shows it when it is still current.
func (m Model) result(msg resultMsg) Model {
	if msg.err == nil {
		m.cache[msg.filter] = msg.days
	}
	if msg.seq != m.seq {
		return m
	}
	m.loading, m.cancel = false, nil
	m.took, m.err = msg.took, msg.err
	if msg.err != nil {
		return m
	}
	m.days, m.loaded = msg.days, true
	m.clampOffset()
	return m
}

// Chrome rows of the body: the switches line, the column header, the rule
// and the status line; the total line comes on top for a week or a month.
const chromeRows = 4

// showTotal reports whether the total line is shown: for a week or a
// month, not for a single day.
func (m Model) showTotal() bool { return m.window.Period != store.PeriodDay }

// rowsAvail is the number of day rows that fit.
func (m Model) rowsAvail() int {
	n := m.height - chromeRows
	if m.showTotal() {
		n--
	}
	return max(n, 0)
}

// clampOffset keeps the scroll offset within the rows.
func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, len(m.days)-m.rowsAvail()))
}

var (
	activeStyle = lipgloss.NewStyle().Reverse(true)
	faintStyle  = lipgloss.NewStyle().Faint(true)
	boldStyle   = lipgloss.NewStyle().Bold(true)
)

// ShortHelp implements help.KeyMap.
func (m Model) ShortHelp() []key.Binding { return m.keys.ShortHelp() }

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// View implements tea.Model: the switches, the header with the time axis,
// one row per day, a rule, the total and the status line.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	line := func(s string) string { return ansi.Truncate(s, m.width, "…") }
	parts := []string{line(m.switches())}
	// The header line (or the hint in its place) is part of the chrome; the
	// day rows fill rowsAvail, the rest is padded.
	pad := m.rowsAvail()
	if hint := m.hint(); hint != "" {
		parts = append(parts, " "+faintStyle.Render(hint))
	} else {
		l := m.layout()
		parts = append(parts, line(" "+l.header()))
		end := min(m.offset+pad, len(m.days))
		for _, d := range m.days[m.offset:end] {
			parts = append(parts, line(" "+l.row(d)))
		}
		pad -= end - m.offset
	}
	for ; pad > 0; pad-- {
		parts = append(parts, "")
	}
	parts = append(parts, strings.Repeat("─", m.width))
	if m.showTotal() {
		parts = append(parts, line(" "+m.total()))
	}
	parts = append(parts, line(" "+m.status()))
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// switches is the top line: the period and the counting mode with the
// active value in brackets, and the window on the right.
func (m Model) switches() string {
	periods := []string{"day", "week", "month"}
	counts := []string{"all", "human"}
	active := 0
	if m.humanOnly {
		active = 1
	}
	left := " period: " + toggles(periods, int(m.window.Period)) + "  count: " + toggles(counts, active)
	right := m.windowLabel()
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// toggles joins the names, the active one in brackets and reverse video.
func toggles(names []string, active int) string {
	out := make([]string, len(names))
	for i, n := range names {
		if i == active {
			n = activeStyle.Render("[" + n + "]")
		}
		out[i] = n
	}
	return strings.Join(out, " ")
}

// windowLabel names the window: its relation to now when it is the
// current or the previous period, the ISO week or the month, and the
// first and last day, e.g. "this week · 2026-W02 · 2026-01-05 – 2026-01-11 ".
func (m Model) windowLabel() string {
	var parts []string
	cur := m.current()
	switch {
	case m.window.Equal(cur):
		parts = append(parts, [...]string{"today", "this week", "this month"}[m.window.Period])
	case m.window.Equal(cur.Shift(-1)):
		parts = append(parts, [...]string{"yesterday", "last week", "last month"}[m.window.Period])
	}
	first, last := m.window.Since(), m.window.Until().AddDate(0, 0, -1)
	switch m.window.Period {
	case store.PeriodDay:
		parts = append(parts, first.Format("Mon 2006-01-02"))
	case store.PeriodWeek:
		y, w := first.ISOWeek()
		parts = append(parts, fmt.Sprintf("%04d-W%02d", y, w), first.Format("2006-01-02")+" – "+last.Format("2006-01-02"))
	default:
		parts = append(parts, first.Format("2006-01"), first.Format("2006-01-02")+" – "+last.Format("2006-01-02"))
	}
	return strings.Join(parts, " · ") + " "
}

// hint is shown instead of the rows.
func (m Model) hint() string {
	switch {
	case m.opts.Loader == nil:
		return "No database open."
	case m.err != nil:
		return "The query failed; see the status line."
	case !m.loaded:
		return "Loading…"
	}
	return ""
}

// total is the sum line of a week or a month.
func (m Model) total() string {
	if !m.loaded {
		return ""
	}
	span, active := store.WorkdayTotal(m.days)
	days := "days"
	if active == 1 {
		days = "day"
	}
	return fmt.Sprintf("total: %d %s with activity, %s", active, days, formatSpan(span))
}

// status is the bottom line: the state of the load and its duration, and
// the visible rows while they do not all fit.
func (m Model) status() string {
	switch {
	case m.opts.Loader == nil:
		return "no database"
	case m.err != nil:
		return "error: " + m.err.Error()
	case m.loading:
		return "loading…"
	case !m.loaded:
		return ""
	}
	s := fmt.Sprintf("%d days · %d ms", len(m.days), m.took.Milliseconds())
	if n := m.rowsAvail(); n < len(m.days) {
		s += fmt.Sprintf(" · rows %d–%d of %d, ↑↓ scroll", m.offset+1, min(m.offset+n, len(m.days)), len(m.days))
	}
	return s
}

// layout is the column widths of the rows for the current data and width.
type layout struct {
	loc         *time.Location
	dayStart    time.Duration
	first, last int // widths of the clock columns
	bar         int // width of the time axis, 0 when there is no room
}

// Fixed columns: "Mon 2006-01-02", the span ("12h34m" at most, right
// aligned) and two spaces between every column.
const (
	dateWidth = 14
	spanWidth = 6
	gap       = 2
)

func (m Model) layout() layout {
	l := layout{loc: m.opts.Location, dayStart: m.opts.DayStart, first: 5, last: 5}
	for _, d := range m.days {
		l.first = max(l.first, len([]rune(d.FirstClock(l.loc))))
		l.last = max(l.last, len([]rune(d.LastClock(l.loc))))
	}
	fixed := 1 + dateWidth + gap + l.first + gap + l.last + gap + spanWidth + gap
	if bar := m.width - fixed - 1; bar >= MinBarWidth {
		l.bar = bar
	}
	return l
}

// header is the column header with the hour labels of the time axis.
func (l layout) header() string {
	s := faintStyle.Render(pad("day", dateWidth) + "  " + pad("first", l.first) + "  " + pad("last", l.last) + "  " + lpad("span", spanWidth))
	if l.bar > 0 {
		s += "  " + faintStyle.Render(l.axis())
	}
	return s
}

// axis labels the time axis every four hours from the day boundary.
func (l layout) axis() string {
	cells := []rune(strings.Repeat(" ", l.bar))
	for i := range 6 {
		t := time.Date(2000, 1, 1, 0, int(l.dayStart.Minutes())+i*4*60, 0, 0, time.UTC)
		label := t.Format("15")
		if t.Minute() != 0 {
			label = t.Format("15:04")
		}
		pos := i * l.bar / 6
		for j, r := range label {
			if pos+j < l.bar {
				cells[pos+j] = r
			}
		}
	}
	return string(cells)
}

// row renders one day: weekday and date, first, last, span and the bar.
func (l layout) row(d store.Workday) string {
	first, last, span := "-", "-", "-"
	if d.Active() {
		first, last, span = d.FirstClock(l.loc), d.LastClock(l.loc), formatSpan(d.Span())
	}
	date := d.Day.Format("Mon 2006-01-02")
	if !d.Active() {
		date = faintStyle.Render(date)
	}
	s := date + "  " + pad(first, l.first) + "  " + pad(last, l.last) + "  " + lpad(span, spanWidth)
	if l.bar > 0 {
		s += "  " + l.barOf(d)
	}
	return s
}

// barOf draws the day's span on the time axis: the axis runs from the
// day boundary to the next one (24 hours, 23 or 25 on a DST change) and
// the bar covers the cells from the first to the last activity, at least
// one cell.
func (l layout) barOf(d store.Workday) string {
	cells := []rune(strings.Repeat("·", l.bar))
	if d.Active() {
		start := store.DayBoundary(d.Day, l.dayStart)
		total := store.DayBoundary(d.Day.AddDate(0, 0, 1), l.dayStart).Sub(start).Seconds()
		pos := func(t time.Time) float64 { return min(max(t.Sub(start).Seconds()/total, 0), 1) }
		a := min(int(pos(d.First)*float64(l.bar)), l.bar-1)
		b := min(max(int(math.Ceil(pos(d.Last)*float64(l.bar))), a+1), l.bar)
		for i := a; i < b; i++ {
			cells[i] = '█'
		}
		return faintStyle.Render(string(cells[:a])) + boldStyle.Render(string(cells[a:b])) + faintStyle.Render(string(cells[b:]))
	}
	return faintStyle.Render(string(cells))
}

// formatSpan formats a span compactly: "5s", "25m", "3h05m".
func formatSpan(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// pad right-pads s with spaces to n cells; lpad left-pads.
func pad(s string, n int) string  { return s + strings.Repeat(" ", max(n-lipgloss.Width(s), 0)) }
func lpad(s string, n int) string { return strings.Repeat(" ", max(n-lipgloss.Width(s), 0)) + s }
