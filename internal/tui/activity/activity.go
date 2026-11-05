// Package activity is the first/last-activity view of the TUI: per day of
// the window (one day, one ISO week or one month) a bar from the first to
// the last activity on a time axis that starts at the configured day
// boundary, the clock times and the span as text, and for a week or a
// month a headline with the working time of the period and a block of
// summary figures.
//
// Each day is measured against the configured working time
// (store.WorkHours): the core hours are a band with lines at its start and
// end on the day's axis, the bar is split into activity within the core
// hours, before or after them, and after midnight, the span is rated
// against the daily target and its difference shown per day. The headline
// of a week or a month sums the spans against the target of its workdays
// (weekdays that are no public holiday, up to today) and rates them with
// the weekly bounds. Weekends and public holidays have no target and their
// own colours; a holiday is named in its row and in the headline. Colours
// come from the theme and follow the terminal background (the app asks
// for it; the answer arrives as tea.BackgroundColorMsg); every coloured
// element also differs in glyph, so NO_COLOR stays legible.
//
// The window is a span: one period moved by a number of days. ↑/↓ scroll
// the rows while not all of them fit and otherwise move the window by a
// day, so the view scrolls through time without a gap; ←/→ jump by a
// whole period. The window never ends after the current period, so the
// view does not run into the future beyond what the current week or month
// shows anyway.
//
// Every change of window or counting mode loads a store.Workday as a
// command; results carry the sequence number of the change they belong
// to, so a slow answer to an older change is dropped. Loaded days are
// cached per filter, so switching back is instant. The chart is text only:
// a row per day is a table as much as a chart, so there is no image path.
//
// The key p colours the bars by project instead (see projects.go): every
// cell takes the project with the most counted events in its time range,
// in the colour the stats view gives the project, with a legend below the
// rows. The project data is a second query per window, cached alike.
package activity

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// MinBarWidth is the narrowest time axis drawn; below it the rows show the
// times only.
const MinBarWidth = 12

// Loader loads the days and their activity by project; *store.Store
// implements it.
type Loader interface {
	Workday(ctx context.Context, f store.WorkdayFilter) ([]store.Workday, error)
	ProjectDays(ctx context.Context, f store.WorkdayFilter) (store.ProjectDays, error)
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
	// Work is the working time the days are measured against; the zero
	// value shows neither core hours nor a target.
	Work store.WorkHours
	// Window is the initial window: "day", "week" or "month", the one
	// containing now; empty means a week. The app validates the name, an
	// unknown one falls back to a week here.
	Window string
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
	// Holidays names the public holidays; nil means there are none.
	Holidays store.Holidays
	// HumanOnly starts the view counting own messages only; the key h
	// switches to all events and back.
	HumanOnly bool
	// Projects starts the view with the bars coloured by project; the key
	// p switches back and forth.
	Projects bool
	// Home is the home directory, abbreviated to "~" in the project names
	// of directories without a repository; empty keeps full paths.
	Home string
	// Colors maps the projects to their colours; share the app's with the
	// stats view, so a project looks alike in both. nil means own colours.
	Colors *chart.Colors
}

func (o *Options) defaults() {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Colors == nil {
		o.Colors = &chart.Colors{}
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
	span      span // the days shown
	humanOnly bool
	figures   bool // the summary figures are shown (week and month)

	seq    uint64
	cancel context.CancelFunc

	cache   map[filter][]store.Workday
	loaded  bool // days holds the data of the current filter
	loading bool
	days    []store.Workday
	took    time.Duration
	err     error
	proj    projects // the bars by project, see projects.go

	offset        int // first visible day row
	initCmd       tea.Cmd
	width, height int
	// th is the theme; dark until the terminal answers the app's
	// background colour query. styles are the view's styles derived from it.
	th     theme.Theme
	styles styles
}

// New returns the view for opts: the configured window containing now,
// counting as opts.HumanOnly says.
func New(opts Options) Model {
	opts.defaults()
	m := Model{
		opts:      opts,
		keys:      newKeyMap(opts.Keys),
		span:      single(initialPeriod(opts.Window), opts.Now(), opts.Location),
		humanOnly: opts.HumanOnly,
		cache:     map[filter][]store.Workday{},
		proj:      projects{on: opts.Projects, cache: map[filter]store.ProjectDays{}},
		figures:   true,
		th:        theme.New(true),
		styles:    newStyles(theme.New(true)),
	}
	m, m.initCmd = m.load()
	return m
}

// initialPeriod parses Options.Window; empty or unknown means a week.
func initialPeriod(name string) store.Period {
	if p, err := store.ParsePeriod(name); err == nil {
		return p
	}
	return store.PeriodWeek
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
	case tea.MouseClickMsg:
		return m.click(msg)
	case tea.MouseWheelMsg:
		if m.opts.Loader == nil {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.up()
		case tea.MouseWheelDown:
			return m.down()
		}
		return m, nil
	case resultMsg:
		return m.result(msg), nil
	case projectsMsg:
		return m.projectsResult(msg), nil
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
		m.styles = newStyles(m.th)
		return m, nil
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
		return m.setSpan(m.span.withPeriod(store.PeriodDay))
	case key.Matches(msg, k.Week):
		return m.setSpan(m.span.withPeriod(store.PeriodWeek))
	case key.Matches(msg, k.Month):
		return m.setSpan(m.span.withPeriod(store.PeriodMonth))
	case key.Matches(msg, k.Earlier):
		return m.setSpan(m.span.periods(-1))
	case key.Matches(msg, k.Later):
		return m.setSpan(m.span.periods(1).clampUntil(m.limit()))
	case key.Matches(msg, k.Current):
		return m.setSpan(m.current())
	case key.Matches(msg, k.Last):
		return m.setSpan(m.current().periods(-1))
	case key.Matches(msg, k.Human):
		m.humanOnly = !m.humanOnly
		return m.load()
	case key.Matches(msg, k.Figures):
		m.figures = !m.figures
		m.clampOffset()
	case key.Matches(msg, k.Projects):
		return m.toggleProjects()
	case key.Matches(msg, k.Up):
		return m.up()
	case key.Matches(msg, k.Down):
		return m.down()
	}
	return m, nil
}

// click handles a left click: on the switches line a period name sets the
// window like its key, a counting mode switches to it (like h, when it is
// not the active one) and a bar mode to that one (like p); on the summary
// figures it hides them like s. Clicks elsewhere do nothing, so do clicks
// without a database.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.opts.Loader == nil {
		return m, nil
	}
	if msg.Y == 0 {
		_, periods, counts, bars := m.switches()
		if i := zone.At(periods, msg.X); i >= 0 {
			return m.setSpan(m.span.withPeriod(periodValues[i]))
		}
		if i := zone.At(counts, msg.X); i >= 0 && (i == 1) != m.humanOnly {
			m.humanOnly = !m.humanOnly
			return m.load()
		}
		if i := zone.At(bars, msg.X); i >= 0 && (i == 1) != m.proj.on {
			return m.toggleProjects()
		}
		return m, nil
	}
	if first := m.height - 1 - figureRows; m.showFigures() && msg.Y >= first && msg.Y < first+figureRows {
		m.figures = false
		m.clampOffset()
	}
	return m, nil
}

// up scrolls the rows up by one while the first is hidden; otherwise it
// moves the window a day back, so the day before the first row appears.
func (m Model) up() (tea.Model, tea.Cmd) {
	if m.offset > 0 {
		m.offset--
		return m, nil
	}
	m.span = m.span.days(-1)
	return m.load()
}

// down scrolls the rows down by one while the last is hidden; otherwise
// it moves the window a day on, keeping the scroll position so the new
// last day appears at the bottom. It stops where the window would end
// after the current period.
func (m Model) down() (tea.Model, tea.Cmd) {
	if m.offset < m.maxOffset() {
		m.offset++
		return m, nil
	}
	if !m.span.until().Before(m.limit()) {
		return m, nil
	}
	m.span = m.span.days(1)
	return m.load()
}

// current is the whole current period containing now.
func (m Model) current() span {
	return single(m.span.period(), m.opts.Now(), m.opts.Location)
}

// limit is the latest end of the window: the end of the current period.
// The current week or month shows its future days anyway, and a day step
// may not run past them.
func (m Model) limit() time.Time { return m.current().until() }

// setSpan shows s from its first row; the same days again are a no-op.
func (m Model) setSpan(s span) (tea.Model, tea.Cmd) {
	if s.equal(m.span) {
		return m, nil
	}
	m.span, m.offset = s, 0
	return m.load()
}

// since and until are the bounds of the window under the day rule: the
// day boundary of its first day and of the day after its last.
func (m Model) since() time.Time { return store.DayBoundary(m.span.since(), m.opts.DayStart) }
func (m Model) until() time.Time { return store.DayBoundary(m.span.until(), m.opts.DayStart) }

// filter is the cache key of the current state.
func (m Model) filter() filter {
	return filter{since: m.since().Unix(), until: m.until().Unix(), humanOnly: m.humanOnly}
}

// load shows the cached days of the current filter, or starts loading
// them, and the same for the project data while the bars are by project.
func (m Model) load() (Model, tea.Cmd) {
	m, cmd := m.loadDays()
	if !m.proj.on {
		return m, cmd
	}
	var pcmd tea.Cmd
	m, pcmd = m.loadProjects()
	return m, tea.Batch(cmd, pcmd)
}

// loadDays shows the cached days of the current filter, or starts loading
// them. A running load is cancelled; its result would be stale.
func (m Model) loadDays() (Model, tea.Cmd) {
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
	m.loaded = false
	return m.fetch(f, false)
}

// Reload drops the cache and loads the current window again, keeping the
// rows and the scroll position on screen until the answer arrives.
// Without a database there is nothing to do.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	if m.opts.Loader == nil {
		return m, nil
	}
	m.cache = map[filter][]store.Workday{}
	m.seq++
	m.stop()
	m.err = nil
	m, cmd := m.fetch(m.filter(), true)
	var pcmd tea.Cmd
	m, pcmd = m.reloadProjects()
	return m, tea.Batch(cmd, pcmd)
}

// fetch starts the query for f with the current sequence number. A quiet
// fetch (reload) does not announce itself in the status line.
func (m Model) fetch(f filter, quiet bool) (Model, tea.Cmd) {
	m.loading = !quiet
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	seq, now, l := m.seq, m.opts.Now, m.opts.Loader
	wf := store.WorkdayFilter{
		Since: m.since(), Until: m.until(), Location: m.opts.Location,
		DayStart: m.opts.DayStart, HumanOnly: m.humanOnly, Holidays: m.opts.Holidays,
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
// and the status line; a week or a month adds the headline and, when
// there is room, the summary figures; the bars by project add the legend.
const chromeRows = 4

// figureRows is the height of the summary figures: first, last and span.
const figureRows = 3

// minDayRows is how many day rows the figures leave at least; below that
// they are hidden.
const minDayRows = 3

// showHeadline reports whether the headline with the working time of the
// period is shown: for a week or a month, not for a single day.
func (m Model) showHeadline() bool { return m.span.period() != store.PeriodDay }

// showFigures reports whether the summary figures are shown: when they
// are on, for a loaded week or month, and when minDayRows day rows stay.
func (m Model) showFigures() bool {
	return m.figures && m.loaded && m.showHeadline() && m.height-chromeRows-1-m.legendRows()-figureRows >= minDayRows
}

// rowsAvail is the number of day rows that fit.
func (m Model) rowsAvail() int {
	n := m.height - chromeRows - m.legendRows()
	if m.showHeadline() {
		n--
	}
	if m.showFigures() {
		n -= figureRows
	}
	return max(n, 0)
}

// maxOffset is the largest scroll offset: the rows that do not fit.
func (m Model) maxOffset() int { return max(0, len(m.days)-m.rowsAvail()) }

// clampOffset keeps the scroll offset within the rows.
func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, m.maxOffset()))
}

// ShortHelp implements help.KeyMap.
func (m Model) ShortHelp() []key.Binding { return m.keys.ShortHelp() }

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// View implements tea.Model: the switches, for a week or a month the
// headline, the header with the time axis, one row per day, a rule, the
// legend of the projects while the bars are by project, the summary
// figures and the status line.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	line := func(s string) string { return ansi.Truncate(s, m.width, "…") }
	switches, _, _, _ := m.switches()
	parts := []string{switches}
	if m.showHeadline() {
		parts = append(parts, line(" "+m.headline()))
	}
	// The header line (or the hint in its place) is part of the chrome; the
	// day rows fill rowsAvail, the rest is padded.
	pad := m.rowsAvail()
	if hint := m.hint(); hint != "" {
		parts = append(parts, " "+m.styles.faint.Render(hint))
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
	parts = append(parts, m.th.Rule.Render(strings.Repeat("─", m.width)))
	if m.proj.on {
		parts = append(parts, line(" "+m.legend()))
	}
	if m.showFigures() {
		for _, f := range m.figureLines() {
			parts = append(parts, line(" "+f))
		}
	}
	parts = append(parts, line(" "+m.status()))
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// Names and values of the switches, in the order shown; the counting
// modes are all events (index 0) and own messages only (index 1), the bar
// modes by working hours (index 0) and by project (index 1).
var (
	periodNames  = []string{"day", "week", "month"}
	periodValues = []store.Period{store.PeriodDay, store.PeriodWeek, store.PeriodMonth}
	countNames   = []string{"all", "human"}
	barNames     = []string{"hours", "projects"}
)

// switches is the top line: the period, the counting mode and the bar mode
// with the active value in brackets, and the window on the right. It
// returns the columns of every period, counting mode and bar mode name,
// clipped like the line, so a click lands on what is shown.
func (m Model) switches() (string, []zone.Span, []zone.Span, []zone.Span) {
	active, bar := 0, 0
	if m.humanOnly {
		active = 1
	}
	if m.proj.on {
		bar = 1
	}
	left := " period: "
	periods, pSpans := zone.Toggles(m.styles.active, periodNames, slices.Index(periodValues, m.span.period()), lipgloss.Width(left))
	left += periods + "  count: "
	counts, cSpans := zone.Toggles(m.styles.active, countNames, active, lipgloss.Width(left))
	left += counts + "  bars: "
	bars, bSpans := zone.Toggles(m.styles.active, barNames, bar, lipgloss.Width(left))
	left += bars
	// The label on the right drops its leading parts (the relation to
	// now, then the ISO week or month) while the line is too wide, falls
	// back to the ISO week or month alone when even the dates do not fit,
	// and is truncated only after that.
	parts := m.windowParts()
	var cands []string
	for i := range parts {
		cands = append(cands, strings.Join(parts[i:], " · "))
	}
	if id := m.windowID(); id != "" {
		cands = append(cands, id)
	}
	right := ""
	for _, c := range cands {
		right = c + " "
		if lipgloss.Width(left)+1+lipgloss.Width(right) <= m.width {
			break
		}
	}
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	line := left + strings.Repeat(" ", gap) + right
	if lipgloss.Width(line) > m.width {
		line = ansi.Truncate(line, m.width, "…")
		w := max(m.width-1, 0)
		pSpans, cSpans, bSpans = zone.Clip(pSpans, w), zone.Clip(cSpans, w), zone.Clip(bSpans, w)
	}
	return line, pSpans, cSpans, bSpans
}

// windowParts names the window, as the parts the switches line joins with
// " · ": its relation to now when it is the current or the previous
// period, the ISO week or the month when the window is a whole one, and
// the first and last day, e.g. "this week", "2026-W02", "2026-01-05 –
// 2026-01-11". A window moved by days shows the days only.
func (m Model) windowParts() []string {
	var parts []string
	cur, p := m.current(), m.span.period()
	switch {
	case m.span.equal(cur):
		parts = append(parts, [...]string{"today", "this week", "this month"}[p])
	case m.span.equal(cur.periods(-1)):
		parts = append(parts, [...]string{"yesterday", "last week", "last month"}[p])
	}
	first, last := m.span.since(), m.span.until().AddDate(0, 0, -1)
	switch p {
	case store.PeriodDay:
		parts = append(parts, first.Format("Mon 2006-01-02"))
		if m.loaded && len(m.days) == 1 && m.days[0].IsHoliday() {
			parts = append(parts, m.styles.holidayText.Render(m.days[0].Holiday))
		}
	default:
		if id := m.windowID(); id != "" {
			parts = append(parts, id)
		}
		parts = append(parts, first.Format("2006-01-02")+" – "+last.Format("2006-01-02"))
	}
	return parts
}

// windowID is the ISO week ("2026-W02") or the month ("2026-01") of the
// window when it is a whole one; empty for a single day or a window moved
// by days.
func (m Model) windowID() string {
	if !m.span.aligned() {
		return ""
	}
	first := m.span.since()
	switch m.span.period() {
	case store.PeriodWeek:
		y, w := first.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	case store.PeriodMonth:
		return first.Format("2006-01")
	}
	return ""
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

// asOf is today as a local midnight under the day rule: later days of the
// window are no workdays yet.
func (m Model) asOf() time.Time { return store.DayOf(m.opts.Now(), m.opts.Location, m.opts.DayStart) }

// headline is the first line of a week or a month: the working time of
// the period, coloured by its rating against the weekly bounds, the
// target (daily target × workdays of the period) and the balance against
// it, and the public holidays of the period by name. Without working
// time it is the days with activity and the sum of their spans. Empty
// until the days are loaded.
func (m Model) headline() string {
	if !m.loaded {
		return ""
	}
	st, w := m.styles, m.opts.Work
	b := w.Balance(m.days, m.asOf())
	label := st.label.Render([...]string{"day:", "week:", "month:"}[m.span.period()])
	parts := []string{st.rating(w.RateBalance(b)).Bold(true).Render(store.FormatSpan(b.Span)) + st.faint.Render(" in "+plural(b.Active, "day"))}
	if !w.IsZero() {
		target := fmt.Sprintf("target %s (%s × %s)", store.FormatSpan(b.Target), plural(b.Workdays, "workday"), store.FormatSpan(w.Target))
		parts = append(parts, st.faint.Render(target), "balance "+st.rating(w.RateBalance(b)).Bold(true).Render(store.FormatDiff(b.Balance)))
	}
	if names := m.holidayNames(); names != "" {
		parts = append(parts, st.holidayText.Render(names))
	}
	return label + " " + strings.Join(parts, st.faint.Render(" · "))
}

// holidayNames lists the public holidays of the loaded days, e.g. "Thu 25
// Weihnachten, Fri 26 Zweiter Weihnachtsfeiertag"; empty without any.
func (m Model) holidayNames() string {
	var names []string
	for _, d := range m.days {
		if d.IsHoliday() {
			names = append(names, d.Day.Format("Mon 02")+" "+d.Holiday)
		}
	}
	return strings.Join(names, ", ")
}

// figureLines are the summary figures of the active days: one line each
// for the first activity, the last activity and the span with their
// median, mean, minimum and maximum. The span figures are coloured by
// their rating against the daily bounds. Without active days the values
// are "-".
func (m Model) figureLines() []string {
	st := m.styles
	sum := store.Summarize(m.days, m.opts.DayStart)
	clock := func(d time.Duration) (string, lipgloss.Style) {
		return store.FormatClock(d, m.opts.DayStart), lipgloss.NewStyle()
	}
	span := func(d time.Duration) (string, lipgloss.Style) {
		return store.FormatSpan(d), st.rating(m.opts.Work.RateSpan(d))
	}
	rows := []struct {
		label  string
		f      store.Figure
		format func(time.Duration) (string, lipgloss.Style)
	}{{"first", sum.First, clock}, {"last", sum.Last, clock}, {"span", sum.Span, span}}
	cols := [][]string{}
	for _, r := range rows {
		vals := []string{st.label.Render(pad(r.label, 5))}
		for i, v := range []time.Duration{r.f.Median, r.f.Mean, r.f.Min, r.f.Max} {
			text, style := "-", lipgloss.NewStyle()
			if sum.Active > 0 {
				text, style = r.format(v)
			}
			vals = append(vals, st.faint.Render([...]string{"median", "mean", "min", "max"}[i])+" "+style.Render(text))
		}
		cols = append(cols, vals)
	}
	// Align the columns across the three lines.
	widths := make([]int, len(cols[0]))
	for _, vals := range cols {
		for i, v := range vals {
			widths[i] = max(widths[i], lipgloss.Width(v))
		}
	}
	out := make([]string, len(cols))
	for j, vals := range cols {
		for i, v := range vals {
			vals[i] = pad(v, widths[i])
		}
		out[j] = strings.TrimRight(strings.Join(vals, "  "), " ")
	}
	return out
}

// plural formats n with the singular or plural noun: "1 day", "3 days".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// countMode names what is counted: "human only" or "all events".
func (m Model) countMode() string {
	if m.humanOnly {
		return "human only"
	}
	return "all events"
}

// status is the bottom line: the state of the load and its duration, the
// counting mode, the public holidays, and the visible rows while they do
// not all fit.
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
	s := fmt.Sprintf("%d days · %d ms · %s", len(m.days), m.took.Milliseconds(), m.countMode())
	if n := m.opts.Work.Balance(m.days, time.Time{}).Holidays; n > 0 {
		s += " · " + plural(n, "holiday")
	}
	if n := m.rowsAvail(); n < len(m.days) {
		s += fmt.Sprintf(" · rows %d–%d of %d, ↑↓ scroll", m.offset+1, min(m.offset+n, len(m.days)), len(m.days))
	}
	return s
}

// layout is the column widths of the rows for the current data and width,
// with the working time and the styles the rows are drawn with, and the
// project data while the bars are by project.
type layout struct {
	loc         *time.Location
	dayStart    time.Duration
	work        store.WorkHours
	styles      styles
	projects    *projectLayout // nil while the bars are by working hours
	first, last int            // widths of the clock columns
	diff        bool           // the difference column is shown (there is a target)
	bar         int            // width of the time axis, 0 when there is no room
}

// Fixed columns: "Mon 2006-01-02", the span ("12h34m" at most, right
// aligned), the difference ("+12h34m") and two spaces between every column.
const (
	dateWidth = 14
	spanWidth = 6
	diffWidth = 7
	gap       = 2
)

func (m Model) layout() layout {
	l := layout{loc: m.opts.Location, dayStart: m.opts.DayStart, work: m.opts.Work, styles: m.styles, projects: m.projectLayout(), first: 5, last: 5, diff: !m.opts.Work.IsZero()}
	for _, d := range m.days {
		l.first = max(l.first, len([]rune(d.FirstClock(l.loc))))
		l.last = max(l.last, len([]rune(d.LastClock(l.loc))))
	}
	fixed := 1 + dateWidth + gap + l.first + gap + l.last + gap + spanWidth + gap
	if l.diff {
		fixed += diffWidth + gap
	}
	if bar := m.width - fixed - 1; bar >= MinBarWidth {
		l.bar = bar
	}
	return l
}

// header is the column header with the hour labels of the time axis.
func (l layout) header() string {
	s := pad("day", dateWidth) + "  " + pad("first", l.first) + "  " + pad("last", l.last) + "  " + lpad("span", spanWidth)
	if l.diff {
		s += "  " + lpad("diff", diffWidth)
	}
	if l.bar > 0 {
		s += "  " + l.axis()
	}
	return l.styles.faint.Render(s)
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

// row renders one day: weekday and date, first, last, span, the
// difference to the target and the bar. A day without activity is faint,
// a weekend in the weekend colour, a public holiday in the holiday colour
// with its name on the axis where the bar leaves room, the span and the
// difference of a workday in the colour of their rating. The bar is by
// project while the layout has the project data.
func (l layout) row(d store.Workday) string {
	st := l.styles
	first, last, span, diff := "-", "-", "-", "-"
	if d.Active() {
		first, last, span = d.FirstClock(l.loc), d.LastClock(l.loc), store.FormatSpan(d.Span())
	}
	if v, ok := l.work.Diff(d); ok {
		diff = store.FormatDiff(v)
	}
	date := d.Day.Format("Mon 2006-01-02")
	var text lipgloss.Style
	switch {
	case d.IsHoliday():
		text = st.holidayText
	case !d.Active():
		text = st.faint
	case store.IsWeekend(d.Day):
		text = st.weekendText
	default:
		text = st.rating(l.work.Rate(d))
	}
	switch {
	case d.IsHoliday():
		date = st.holidayText.Render(date)
	case !d.Active():
		date = st.faint.Render(date)
	case store.IsWeekend(d.Day):
		date = st.weekendText.Render(date)
	}
	s := date + "  " + pad(first, l.first) + "  " + pad(last, l.last) + "  " + text.Render(lpad(span, spanWidth))
	if l.diff {
		s += "  " + text.Render(lpad(diff, diffWidth))
	}
	if l.bar > 0 {
		cells := l.cells(d)
		if l.projects != nil {
			s += "  " + l.projects.bar(l, cells, d)
		} else {
			s += "  " + l.renderBar(cells, l.caption(cells, d))
		}
	}
	return s
}

// pad right-pads s with spaces to n cells; lpad left-pads.
func pad(s string, n int) string  { return s + strings.Repeat(" ", max(n-lipgloss.Width(s), 0)) }
func lpad(s string, n int) string { return strings.Repeat(" ", max(n-lipgloss.Width(s), 0)) + s }
