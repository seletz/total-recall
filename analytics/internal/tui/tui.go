// Package tui is the terminal view of total-recall-analytics: one view of
// the usage of the pool over time. Toggle rows choose what is counted
// (metric), how it is split (by), the period, the host and the window of
// periods; a stacked bar chart shows the window, a table its totals. The
// numbers come from package report, the same queries as the commands
// usage, tokens and worktime. Everything is reachable with the keys and
// the mouse: every toggle, the window arrows and every hint of the footer
// are clickable, the wheel moves the window.
package tui

import (
	"cmp"
	"context"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/analytics/internal/report"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// Loader opens the analytics database for one load and runs run on it,
// then closes it again: DuckDB lets only one process write the file, and
// a TUI that kept it open would block the next build.
type Loader func(ctx context.Context, run func(report.Querier) error) error

// Options configure the view.
type Options struct {
	Load Loader
	// Path is the analytics database, shown in the header; Home is
	// abbreviated to "~" in it.
	Path, Home string
	// TZ is the primary time zone of the database, shown in the header.
	TZ string
	// Metric, By, Period and Host are the starting values (names as in
	// the toggle rows; Host "" is every host). Unknown names fall back to
	// the first value.
	Metric, By, Period, Host string
	// NoMouse leaves the terminal's mouse reports off.
	NoMouse bool
}

// metric is one choice of the show row.
type metric struct {
	name string
	// worktime metrics come from report.WorktimeRows, the rest from
	// report.UsageRows.
	worktime bool
	usage    func(report.Usage) float64
}

var metrics = []metric{
	{name: "events", usage: func(u report.Usage) float64 { return float64(u.Human + u.Agent) }},
	{name: "human", usage: func(u report.Usage) float64 { return float64(u.Human) }},
	{name: "agent", usage: func(u report.Usage) float64 { return float64(u.Agent) }},
	{name: "sessions", usage: func(u report.Usage) float64 { return float64(u.Sessions) }},
	{name: "tool calls", usage: func(u report.Usage) float64 { return float64(u.ToolCalls) }},
	{name: "tokens", usage: func(u report.Usage) float64 { return float64(u.TokensIn + u.TokensOut) }},
	{name: "hours", worktime: true},
}

// Groupings of the by row: the usage metrics and the work time.
var (
	usageBy    = []string{"project", "host", "model", "tool", "account"}
	worktimeBy = []string{"category", "project", "host"}
	periods    = []string{"day", "week", "month"}
)

// topKeys is how many keys get a series of their own; the rest is other.
const topKeys = 6

// Rows of the layout, counted from the top of the screen.
const (
	rowHeader = iota
	rowRule
	rowMetric
	rowBy
	rowPeriod
	rowHost
	rowRange
	rowBody // first row of the chart
)

// bucket is one period of the axis.
type bucket struct {
	key        string // the period as report formats it: 2026-10-05, 2026-W41, 2026-10
	start, end time.Time
}

// Model is the view.
type Model struct {
	opts          Options
	th            theme.Theme
	keys          keyMap
	width, height int

	metric, by, period, host int
	hosts                    []string
	first, last              string // days of the database

	buckets []bucket
	// values are the metric per period and key, keys the keys ranked by
	// their total over all periods (nil key = "").
	values map[string]map[string]float64
	keys2  []string
	// offset is the index of the first bucket shown; follow keeps the
	// window at the end until the user moves it.
	offset int
	follow bool
	// totals are the exact totals of the window per key and over all
	// keys (sessions are counted once per window, not per period).
	totals map[string]float64
	total  float64

	colors  *chart.Colors
	err     error
	loading bool
	help    bool
}

// New returns the view.
func New(opts Options) Model {
	m := Model{opts: opts, th: theme.New(true), keys: defaultKeys(), colors: &chart.Colors{}, follow: true}
	m.metric = max(slices.IndexFunc(metrics, func(x metric) bool { return x.name == opts.Metric }), 0)
	m.period = max(slices.Index(periods, opts.Period), 0)
	if opts.Period == "" {
		m.period = 1 // weeks
	}
	m.by = max(slices.Index(m.byNames(), opts.By), 0)
	return m
}

func (m Model) byNames() []string {
	if metrics[m.metric].worktime {
		return worktimeBy
	}
	return usageBy
}

// hostNames are the choices of the host row: all, then every host.
func (m Model) hostNames() []string { return append([]string{"all"}, m.hosts...) }

// filter is the report filter of the current choices over all days.
func (m Model) filter() report.Filter {
	f := report.Filter{By: m.byNames()[m.by], Period: periods[m.period]}
	if m.host > 0 && m.host <= len(m.hosts) {
		f.Hosts = []string{m.hosts[m.host-1]}
	}
	return f
}

// Init loads the data.
func (m Model) Init() tea.Cmd { return m.load(true) }

// loadedMsg carries the result of a load.
type loadedMsg struct {
	hosts       []string
	first, last string
	values      map[string]map[string]float64
	keys        []string
	err         error
}

// totalsMsg carries the totals of a window.
type totalsMsg struct {
	totals map[string]float64
	total  float64
	err    error
	window [2]string
}

// load reads the series of the current choices (and the hosts and days
// with withMeta).
func (m Model) load(withMeta bool) tea.Cmd {
	f, mt := m.filter(), metrics[m.metric]
	load := m.opts.Load
	return func() tea.Msg {
		var msg loadedMsg
		msg.err = load(context.Background(), func(db report.Querier) error {
			ctx := context.Background()
			if withMeta {
				var err error
				if msg.hosts, err = report.Hosts(ctx, db); err != nil {
					return err
				}
				if msg.first, msg.last, err = report.Days(ctx, db); err != nil {
					return err
				}
			}
			var err error
			msg.values, msg.keys, err = series(ctx, db, f, mt)
			return err
		})
		return msg
	}
}

// series reads the metric per period and key and ranks the keys.
func series(ctx context.Context, db report.Querier, f report.Filter, mt metric) (map[string]map[string]float64, []string, error) {
	values := map[string]map[string]float64{}
	totals := map[string]float64{}
	add := func(period string, k *string, v float64) {
		key := ""
		if k != nil {
			key = *k
		}
		if values[period] == nil {
			values[period] = map[string]float64{}
		}
		values[period][key] += v
		totals[key] += v
	}
	if mt.worktime {
		rows, err := report.WorktimeRows(ctx, db, f)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			add(r.Period, r.Key, float64(r.RawSeconds)/3600)
		}
	} else {
		rows, err := report.UsageRows(ctx, db, f)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			add(r.Period, r.Key, mt.usage(r))
		}
	}
	keys := make([]string, 0, len(totals))
	for k, v := range totals {
		if v != 0 { // a key without any amount has nothing to show
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Or(cmp.Compare(totals[b], totals[a]), strings.Compare(a, b)) })
	return values, keys, nil
}

// loadTotals reads the exact totals of the window shown.
func (m Model) loadTotals() tea.Cmd {
	if len(m.buckets) == 0 {
		return nil
	}
	lo, hi := m.window()
	f, mt := m.filter(), metrics[m.metric]
	f.Period = "total"
	f.Since, f.Until = m.buckets[lo].start.Format(time.DateOnly), m.buckets[hi-1].end.AddDate(0, 0, -1).Format(time.DateOnly)
	window := [2]string{f.Since, f.Until}
	load := m.opts.Load
	return func() tea.Msg {
		msg := totalsMsg{totals: map[string]float64{}, window: window}
		msg.err = load(context.Background(), func(db report.Querier) error {
			ctx := context.Background()
			values, _, err := series(ctx, db, f, mt)
			if err != nil {
				return err
			}
			for k, v := range values["total"] {
				msg.totals[k] = v
			}
			f.By = "none"
			all, _, err := series(ctx, db, f, mt)
			if err != nil {
				return err
			}
			msg.total = all["total"][""]
			return nil
		})
		return msg
	}
}

// Update handles keys, mouse and the loads.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampOffset()
		return m, m.loadTotals()
	case loadedMsg:
		m.loading = false
		if m.err = msg.err; m.err != nil {
			return m, nil
		}
		if msg.first != "" {
			first := m.hosts == nil
			m.hosts, m.first, m.last = msg.hosts, msg.first, msg.last
			if i := slices.Index(m.hosts, m.opts.Host); first && i >= 0 && m.host == 0 {
				// The starting host is known only now; load its numbers.
				m.host = i + 1
				m.loading = true
				return m, m.load(false)
			}
		}
		m.values, m.keys2 = msg.values, msg.keys
		m.buckets = buckets(m.first, m.last, periods[m.period])
		m.clampOffset()
		return m, m.loadTotals()
	case totalsMsg:
		if m.err = msg.err; m.err == nil {
			m.totals, m.total = msg.totals, msg.total
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg.String())
	case tea.MouseClickMsg:
		return m.click(msg)
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp, tea.MouseWheelLeft:
			return m.move(-1)
		case tea.MouseWheelDown, tea.MouseWheelRight:
			return m.move(1)
		}
	}
	return m, nil
}

// key runs the action of a key.
func (m Model) key(k string) (tea.Model, tea.Cmd) {
	is := func(b key.Binding) bool { return slices.Contains(b.Keys(), k) }
	switch {
	case m.help && (is(m.keys.Help) || is(m.keys.Back)):
		m.help = false
		return m, nil
	case is(m.keys.Quit):
		return m, tea.Quit
	case m.help:
		return m, nil
	case is(m.keys.Help):
		m.help = true
		return m, nil
	case is(m.keys.Metric):
		return m.setMetric((m.metric + 1) % len(metrics))
	case is(m.keys.MetricBack):
		return m.setMetric((m.metric + len(metrics) - 1) % len(metrics))
	case is(m.keys.By):
		return m.set(func(m *Model) { m.by = (m.by + 1) % len(m.byNames()) })
	case is(m.keys.Period):
		return m.set(func(m *Model) { m.period = (m.period + 1) % len(periods) })
	case is(m.keys.Host):
		return m.set(func(m *Model) { m.host = (m.host + 1) % len(m.hostNames()) })
	case is(m.keys.Earlier):
		return m.move(-1)
	case is(m.keys.Later):
		return m.move(1)
	case is(m.keys.First):
		return m.move(-len(m.buckets))
	case is(m.keys.Last):
		return m.move(len(m.buckets))
	case is(m.keys.Reload):
		m.loading = true
		return m, m.load(true)
	}
	return m, nil
}

// setMetric changes the metric and keeps the grouping when the new metric
// has it.
func (m Model) setMetric(i int) (tea.Model, tea.Cmd) {
	by := m.byNames()[m.by]
	m.metric = i
	m.by = max(slices.Index(m.byNames(), by), 0)
	m.loading = true
	return m, m.load(false)
}

// set changes one choice of m and reloads.
func (m Model) set(change func(*Model)) (tea.Model, tea.Cmd) {
	change(&m)
	m.loading = true
	return m, m.load(false)
}

// move shifts the window by pages (a page is half the window).
func (m Model) move(pages int) (tea.Model, tea.Cmd) {
	n := m.visible()
	step := max(n/2, 1)
	m.offset += pages * step
	m.follow = false
	m.clampOffset()
	if lo, _ := m.window(); lo+n >= len(m.buckets) {
		m.follow = true
	}
	return m, m.loadTotals()
}

// visible is how many periods the chart shows: bars two columns wide
// with a gap, after the value axis.
func (m Model) visible() int {
	if m.width == 0 {
		return len(m.buckets)
	}
	return max((m.width-chartGutter+1)/3, 1)
}

// chartGutter is the room the value axis of the chart takes at most.
const chartGutter = 14

func (m *Model) clampOffset() {
	n := m.visible()
	maxOff := max(len(m.buckets)-n, 0)
	if m.follow {
		m.offset = maxOff
	}
	m.offset = min(max(m.offset, 0), maxOff)
}

// window is the range of buckets shown, [lo, hi).
func (m Model) window() (int, int) {
	lo := m.offset
	return lo, min(lo+m.visible(), len(m.buckets))
}

// buckets lists every period from first to last (YYYY-MM-DD).
func buckets(first, last, period string) []bucket {
	a, err1 := time.Parse(time.DateOnly, first)
	b, err2 := time.Parse(time.DateOnly, last)
	if err1 != nil || err2 != nil || b.Before(a) {
		return nil
	}
	var out []bucket
	switch period {
	case "week":
		a = a.AddDate(0, 0, -((int(a.Weekday()) + 6) % 7))
		for t := a; !t.After(b); t = t.AddDate(0, 0, 7) {
			y, w := t.ISOWeek()
			out = append(out, bucket{key: fmt.Sprintf("%d-W%02d", y, w), start: t, end: t.AddDate(0, 0, 7)})
		}
	case "month":
		a = time.Date(a.Year(), a.Month(), 1, 0, 0, 0, 0, time.UTC)
		for t := a; !t.After(b); t = t.AddDate(0, 1, 0) {
			out = append(out, bucket{key: t.Format("2006-01"), start: t, end: t.AddDate(0, 1, 0)})
		}
	default:
		for t := a; !t.After(b); t = t.AddDate(0, 0, 1) {
			out = append(out, bucket{key: t.Format(time.DateOnly), start: t, end: t.AddDate(0, 0, 1)})
		}
	}
	return out
}

// storePeriod maps a period name to the period of the chart axis.
func storePeriod(p string) store.Period {
	switch p {
	case "week":
		return store.PeriodWeek
	case "month":
		return store.PeriodMonth
	}
	return store.PeriodDay
}

// series keys shown: the top keys and other.
func (m Model) shownKeys() (top []string, other bool) {
	top = m.keys2
	if len(top) > topKeys {
		top, other = top[:topKeys], true
	}
	return top, other
}

// keyName is how a key is shown: "(none)" for the empty key.
func keyName(k string) string {
	if k == "" {
		return "(none)"
	}
	return k
}

// spec is the chart of the window.
func (m Model) spec() chart.Spec {
	lo, hi := m.window()
	mt := metrics[m.metric]
	sp := chart.Spec{Title: mt.name + " by " + m.byNames()[m.by], Period: storePeriod(periods[m.period])}
	for _, b := range m.buckets[lo:hi] {
		sp.Labels = append(sp.Labels, chart.AxisLabel(b.start, sp.Period))
		if sp.Period == store.PeriodDay {
			sp.Marks = append(sp.Marks, chart.Mark{Weekend: store.IsWeekend(b.start)})
		}
	}
	top, other := m.shownKeys()
	for _, k := range top {
		s := chart.Series{Key: k, Name: keyName(k), Values: make([]float64, hi-lo)}
		for j, b := range m.buckets[lo:hi] {
			s.Values[j] = m.values[b.key][k]
		}
		sp.Series = append(sp.Series, s)
	}
	if other {
		s := chart.Series{Key: store.OtherKey, Name: store.OtherKey, Color: chart.OtherColor, Values: make([]float64, hi-lo)}
		for j, b := range m.buckets[lo:hi] {
			for k, v := range m.values[b.key] {
				if !slices.Contains(top, k) {
					s.Values[j] += v
				}
			}
		}
		sp.Series = append(sp.Series, s)
	}
	return m.colors.Assign(sp)
}

// View draws the screen.
func (m Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	if !m.opts.NoMouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if m.width == 0 || m.height == 0 {
		return v
	}
	rule := m.th.Rule.Render(strings.Repeat("─", m.width))
	lines := []string{m.header(), rule}
	for _, r := range m.controls() {
		lines = append(lines, m.line(r.text))
	}
	bodyH := m.height - len(lines) - 2 // rule and footer
	body := m.body(bodyH)
	if m.help {
		body = m.helpPanel(bodyH)
	}
	lines = append(lines, body...)
	footer, _ := m.footer()
	lines = append(lines, rule, footer)
	v.Content = strings.Join(lines, "\n")
	return v
}

// line cuts s to the width.
func (m Model) line(s string) string { return ansi.Truncate(s, m.width, "…") }

// header is the program name, the database and its hosts and zone.
func (m Model) header() string {
	path := m.opts.Path
	if m.opts.Home != "" {
		if rel, ok := strings.CutPrefix(path, m.opts.Home+"/"); ok {
			path = "~/" + rel
		}
	}
	info := fmt.Sprintf("%s · %d hosts · %s", path, len(m.hosts), m.opts.TZ)
	if m.loading {
		info += " · loading…"
	}
	return m.line(" " + m.th.Gradient("total-recall-analytics") + "  " + m.th.Muted.Render(info))
}

// control is one toggle row with the spans of its choices.
type control struct {
	text  string
	spans []zone.Span
}

// labelWidth is the width of the row labels, with the space after them.
const labelWidth = 9

// controls are the rows metric, by, period, host and range.
func (m Model) controls() []control {
	row := func(label string, names []string, active int) control {
		text, spans := zone.Toggles(m.th.Active, names, active, 1+labelWidth)
		return control{text: " " + m.th.Label.Render(fmt.Sprintf("%-*s", labelWidth, label)) + text, spans: spans}
	}
	names := make([]string, len(metrics))
	for i, x := range metrics {
		names[i] = x.name
	}
	out := []control{
		row("show", names, m.metric),
		row("by", m.byNames(), m.by),
		row("period", periods, m.period),
		row("host", m.hostNames(), m.host),
	}
	// range: ← first … last →, then how many periods of how many.
	lo, hi := m.window()
	text := " " + m.th.Label.Render(fmt.Sprintf("%-*s", labelWidth, "range"))
	if hi > lo {
		x := 1 + labelWidth
		left, right := "←", "→"
		span := fmt.Sprintf(" %s … %s ", m.buckets[lo].key, m.buckets[hi-1].key)
		text += m.th.AccentText.Render(left) + span + m.th.AccentText.Render(right) +
			m.th.Muted.Render(fmt.Sprintf("  %d of %d %ss", hi-lo, len(m.buckets), periods[m.period]))
		out = append(out, control{text: text, spans: []zone.Span{{Start: x, End: x + 1}, {Start: x + 1 + lipgloss.Width(span), End: x + 2 + lipgloss.Width(span)}}})
	} else {
		out = append(out, control{text: text + m.th.Muted.Render("no data")})
	}
	return out
}

// body is the chart and the table below it, h lines.
func (m Model) body(h int) []string {
	out := []string{""}
	switch {
	case m.err != nil:
		out = append(out, " "+m.th.Error.Render(m.err.Error()))
	case len(m.buckets) == 0:
		out = append(out, " "+m.th.Muted.Render("no data: run total-recall-analytics build first"))
	default:
		top, other := m.shownKeys()
		tableH := len(top) + 2 // header and all
		if other {
			tableH++
		}
		chartH := max(h-tableH-2, 5)
		for _, l := range strings.Split(chart.RenderText(m.spec(), m.width-2, chartH), "\n") {
			out = append(out, m.line(" "+l))
		}
		out = append(out, "")
		out = append(out, m.table(top, other)...)
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// table lists the totals of the window per key shown, other and all.
func (m Model) table(top []string, other bool) []string {
	mt := metrics[m.metric]
	format := func(v float64) string {
		if mt.worktime {
			return fmt.Sprintf("%d:%02d", int(v), int(v*60+0.5)%60)
		}
		return fmt.Sprintf("%.0f", v)
	}
	sp := m.spec()
	colorOf := map[string]color.Color{}
	for _, s := range sp.Series {
		colorOf[s.Key] = s.Color
	}
	nameW := len("all")
	for _, k := range top {
		nameW = max(nameW, lipgloss.Width(keyName(k)))
	}
	nameW = min(nameW, max(m.width/2, 10))
	head := strings.ToUpper(m.byNames()[m.by])
	unit := strings.ToUpper(mt.name)
	lo, hi := m.window()
	out := []string{m.line(" " + m.th.Label.Render(fmt.Sprintf("  %-*s  %12s", nameW, head, unit)) + m.th.Muted.Render(fmt.Sprintf("  %s … %s", m.buckets[lo].key, m.buckets[hi-1].key)))}
	row := func(swatch color.Color, name string, v float64) {
		sw := " "
		if swatch != nil {
			sw = lipgloss.NewStyle().Foreground(swatch).Render("■")
		}
		out = append(out, m.line(" "+sw+" "+fmt.Sprintf("%-*s  %12s", nameW, ansi.Truncate(name, nameW, "…"), format(v))))
	}
	for _, k := range top {
		row(colorOf[k], keyName(k), m.totals[k])
	}
	if other {
		// Sessions and the like are not additive over keys (a session can
		// use several models); other is what the keys shown leave of all.
		sum := 0.0
		for k, v := range m.totals {
			if !slices.Contains(top, k) {
				sum += v
			}
		}
		row(chart.OtherColor, store.OtherKey, sum)
	}
	out = append(out, m.line(" "+m.th.Strong.Render(fmt.Sprintf("  %-*s  %12s", nameW, "all", format(m.total)))))
	return out
}

// hint is one clickable part of the footer.
type hint struct {
	key  string
	span zone.Span
}

// footer renders the key hints: the actions of the view first, then help
// and quit, separated like the footer of total-recall's TUI.
func (m Model) footer() (string, []hint) {
	st := m.th.Help()
	bindings := m.keys.short()
	if m.help {
		bindings = []key.Binding{m.keys.Back, m.keys.Quit}
	}
	sep := st.ShortSeparator.Inline(true).Render(" • ")
	var b strings.Builder
	var hints []hint
	x := 1
	for i, kb := range bindings {
		item := st.ShortKey.Inline(true).Render(kb.Help().Key) + " " + st.ShortDesc.Inline(true).Render(kb.Help().Desc)
		if i > 0 {
			b.WriteString(sep)
			x += lipgloss.Width(sep)
		}
		w := lipgloss.Width(item)
		if x+w > m.width-1 {
			b.WriteString(st.Ellipsis.Inline(true).Render("…"))
			break
		}
		hints = append(hints, hint{key: kb.Keys()[0], span: zone.Span{Start: x, End: x + w}})
		b.WriteString(item)
		x += w
	}
	return m.line(" " + b.String()), hints
}

// helpPanel lists every key with what it does.
func (m Model) helpPanel(h int) []string {
	hp := help.New()
	hp.Styles = m.th.Help()
	hp.ShowAll = true
	panel := lipgloss.JoinVertical(lipgloss.Left, m.th.Title.Render("keys: usage"), "", hp.FullHelpView(m.keys.full()), "",
		m.th.Muted.Render("esc or ? closes this help; every toggle, the arrows and the hints are also clickable"))
	box := m.th.Border.Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(panel)
	placed := lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
	return strings.Split(placed, "\n")
}

// click runs what is under the pointer: a toggle, a window arrow or a
// footer hint.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if msg.Y == m.height-1 {
		_, hints := m.footer()
		for _, h := range hints {
			if h.span.Contains(msg.X) {
				return m.key(h.key)
			}
		}
		return m, nil
	}
	if m.help {
		return m, nil
	}
	ctl := m.controls()
	i := msg.Y - rowMetric
	if i < 0 || i >= len(ctl) {
		return m, nil
	}
	hit := zone.At(ctl[i].spans, msg.X)
	if hit < 0 {
		return m, nil
	}
	switch msg.Y {
	case rowMetric:
		return m.setMetric(hit)
	case rowBy:
		return m.set(func(m *Model) { m.by = hit })
	case rowPeriod:
		return m.set(func(m *Model) { m.period = hit })
	case rowHost:
		return m.set(func(m *Model) { m.host = hit })
	case rowRange:
		if hit == 0 {
			return m.move(-1)
		}
		return m.move(1)
	}
	return m, nil
}
