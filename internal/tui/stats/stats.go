// Package stats is the activity statistics view of the TUI: a stacked bar
// chart of the activity per period (day, week, month) and dimension
// (project, agent, directory) over a window of periods, the legend and a
// table of the same series.
//
// Every change of period, dimension or window loads a store.ActivitySeries
// as a command; results carry the sequence number of the change they belong
// to, so a slow answer to an older change is dropped from the view. Loaded
// series are cached per filter combination, so switching back is instant.
// The metric (events, human events, sessions) is a property of the chart
// and never loads.
//
// The heatmap (weekday × hour, key h) is a second, optional chart below the
// bar chart. Its data is a store.Stats of the window, loaded and cached the
// same way, but only while the heatmap is on.
package stats

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

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/window"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// TopN is how many dimension values the chart stacks; the rest is "other".
const TopN = 6

// MinChartHeight is the height the chart keeps when the table has to shrink.
const MinChartHeight = 8

// chromeRows are the rows of the body beside chart and table: the switches
// line, the rule above the table and the status line. The tiles with the
// key figures take one more row when the chart keeps MinChartHeight
// beside them (see layout).
const chromeRows = 3

// Loader loads activity series and stats; *store.Store implements it.
type Loader interface {
	ActivitySeries(ctx context.Context, f store.ActivityFilter) (store.ActivitySeries, error)
	Stats(ctx context.Context, f store.StatsFilter) (store.Stats, error)
}

// Options configure the view.
type Options struct {
	// Loader answers the queries; nil means no database, the view shows a hint.
	Loader Loader
	// Location is the time zone of the period boundaries; nil means UTC.
	Location *time.Location
	// Now is the clock the default window ends with; nil means time.Now.
	Now func() time.Time
	// Chart draws the chart area; nil means the container Graphics selects.
	Chart Chart
	// Graphics selects the chart container when Chart is nil: empty means a
	// plain TextChart that never touches the terminal; auto, text or kitty
	// mean a KittyChart starting in that mode (see NewKittyChart).
	Graphics Graphics
	// Home is the home directory, abbreviated to "~" in the project names
	// of directories without a repository; empty keeps full paths.
	Home string
	// Period and Dimension are the initial period and dimension; the zero
	// values are days by project.
	Period    store.Period
	Dimension store.Dimension
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
	// Holidays names the public holidays marked on the day axis; nil
	// means there are none.
	Holidays store.Holidays
	// Projects assigns the keys of the project dimension to the configured
	// projects ([[project]]) and supplies their colours; the zero value
	// leaves the keys and uses the palette.
	Projects project.Resolver
	// Colors maps the series keys to their colours; share the app's with
	// the activity view, so a project looks alike in both. nil means own
	// colours.
	Colors *chart.Colors
}

func (o *Options) defaults() {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Chart == nil && o.Graphics != "" {
		o.Chart = NewKittyChart(KittyOptions{Mode: o.Graphics})
	}
	if o.Chart == nil {
		o.Chart = NewTextChart()
	}
	if o.Colors == nil {
		o.Colors = &chart.Colors{}
	}
}

// filter is one cache key: the parameters of a load.
type filter struct {
	dim    store.Dimension
	period store.Period
	since  int64 // unix seconds
	until  int64
}

// resultMsg is the answer of one load; it is shown only when seq is still
// the current sequence, and cached in any case.
type resultMsg struct {
	seq    uint64
	filter filter
	series store.ActivitySeries
	took   time.Duration
	err    error
}

// heatFilter is one cache key of the heatmap: the window.
type heatFilter struct {
	since int64 // unix seconds
	until int64
}

// heatResultMsg is the answer of one heatmap load; it is shown only when
// seq is still the current heatmap sequence, and cached in any case.
type heatResultMsg struct {
	seq    uint64
	filter heatFilter
	stats  store.Stats
	err    error
}

// heatmap is the state of the optional second chart.
type heatmap struct {
	on      bool
	seq     uint64 // counts changes of the heatmap filter and its toggling
	cancel  context.CancelFunc
	cache   map[heatFilter]store.Stats
	loaded  bool // stats holds the data of the current window
	loading bool
	stats   store.Stats
	err     error
}

// Model is the stats view.
type Model struct {
	opts   Options
	keys   KeyMap
	dim    store.Dimension
	window window.Window
	metric chart.Metric

	// seq counts changes of the filter; results carry the value they were
	// started for and are ignored when it moved on.
	seq    uint64
	cancel context.CancelFunc

	cache   map[filter]store.ActivitySeries
	loaded  bool // series holds the data of the current filter
	loading bool
	series  store.ActivitySeries
	spec    chart.Spec
	colors  *chart.Colors
	took    time.Duration
	err     error

	heat heatmap

	chart         Chart
	initCmd       tea.Cmd // the first load, started by Init
	width, height int
	// th is the theme; dark until the terminal answers the app's
	// background colour query.
	th theme.Theme
}

// New returns the view for opts: the initial period and dimension (days by
// project unless configured), the default window ending today, metric
// events.
func New(opts Options) Model {
	opts.defaults()
	m := Model{
		opts:   opts,
		keys:   newKeyMap(opts.Keys),
		dim:    opts.Dimension,
		window: window.New(opts.Period, opts.Now(), opts.Location),
		cache:  map[filter]store.ActivitySeries{},
		colors: opts.Colors,
		chart:  opts.Chart,
		heat:   heatmap{cache: map[heatFilter]store.Stats{}},
		th:     theme.New(true),
	}
	// The first load is prepared here because Init cannot change the model:
	// the command must carry the sequence number the model will compare
	// against.
	m, m.initCmd = m.load()
	return m
}

// Title implements tui.View.
func (Model) Title() string { return "stats" }

// Init implements tea.Model: it starts the first load and the chart.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.initCmd, m.chart.Init())
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.chart.SetSize(m.chartSize())
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseClickMsg:
		return m.click(msg)
	case resultMsg:
		return m.result(msg)
	case heatResultMsg:
		return m.heatResult(msg), nil
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
		return m, nil
	}
	var cmd tea.Cmd
	m.chart, cmd = m.chart.Update(msg)
	return m, cmd
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if key.Matches(msg, k.Graphics) {
		// The mode is worth switching without a database too: the status
		// line shows the outcome.
		if t, ok := m.chart.(GraphicsToggler); ok {
			return m, t.Toggle()
		}
		return m, nil
	}
	if m.opts.Loader == nil {
		return m, nil
	}
	switch {
	case key.Matches(msg, k.Day):
		return m.setPeriod(store.PeriodDay)
	case key.Matches(msg, k.Week):
		return m.setPeriod(store.PeriodWeek)
	case key.Matches(msg, k.Month):
		return m.setPeriod(store.PeriodMonth)
	case key.Matches(msg, k.Project):
		return m.setDimension(store.DimProject)
	case key.Matches(msg, k.Agent):
		return m.setDimension(store.DimAgent)
	case key.Matches(msg, k.Directory):
		return m.setDimension(store.DimDirectory)
	case key.Matches(msg, k.Earlier):
		return m.setWindow(m.window.Shift(-1))
	case key.Matches(msg, k.Later):
		return m.setWindow(m.window.Shift(1))
	case key.Matches(msg, k.ZoomOut):
		return m.setWindow(m.window.Zoom(-1))
	case key.Matches(msg, k.ZoomIn):
		return m.setWindow(m.window.Zoom(1))
	case key.Matches(msg, k.Metric):
		m.metric = (m.metric + 1) % 3
		return m, m.rebuild()
	case key.Matches(msg, k.Heatmap):
		return m.toggleHeatmap()
	}
	return m, nil
}

// click handles a left click on the switches line: a period name sets the
// period, a dimension name the dimension, exactly like their keys. Clicks
// elsewhere do nothing, so do clicks without a database.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || msg.Y != 0 || m.opts.Loader == nil {
		return m, nil
	}
	_, periods, dims := m.switches()
	if i := zone.At(periods, msg.X); i >= 0 {
		return m.setPeriod(periodValues[i])
	}
	if i := zone.At(dims, msg.X); i >= 0 {
		return m.setDimension(dimensionValues[i])
	}
	return m, nil
}

// toggleHeatmap switches the heatmap on, loading its data, or off,
// cancelling a running load. The bar chart is resized either way.
func (m Model) toggleHeatmap() (tea.Model, tea.Cmd) {
	m.heat.on = !m.heat.on
	var cmd tea.Cmd
	if m.heat.on {
		m, cmd = m.loadHeat()
	} else {
		m.heat.seq++
		m.stopHeat()
	}
	return m, tea.Batch(cmd, m.chart.SetSize(m.chartSize()))
}

func (m Model) setPeriod(p store.Period) (tea.Model, tea.Cmd) {
	if p == m.window.Period {
		return m, nil
	}
	return m.setWindow(m.window.WithPeriod(p))
}

func (m Model) setDimension(d store.Dimension) (tea.Model, tea.Cmd) {
	if d == m.dim {
		return m, nil
	}
	m.dim = d
	return m.load()
}

func (m Model) setWindow(w window.Window) (tea.Model, tea.Cmd) {
	if w.Equal(m.window) {
		return m, nil
	}
	m.window = w
	var cmd, heatCmd tea.Cmd
	m, cmd = m.load()
	if m.heat.on {
		m, heatCmd = m.loadHeat()
	}
	return m, tea.Batch(cmd, heatCmd)
}

// filter is the cache key of the current state.
func (m Model) filter() filter {
	return filter{dim: m.dim, period: m.window.Period, since: m.window.Since().Unix(), until: m.window.Until().Unix()}
}

// load shows the cached series of the current filter, or starts loading it.
// A running load is cancelled; its result would be stale.
func (m Model) load() (Model, tea.Cmd) {
	m.seq++
	m.stop()
	m.err = nil
	if m.opts.Loader == nil {
		return m, nil
	}
	f := m.filter()
	if s, ok := m.cache[f]; ok {
		m.series, m.loaded, m.took = s, true, 0
		return m, m.rebuild()
	}
	m.loaded = false
	return m.fetch(f, false)
}

// Reload drops the caches and loads the current filter (and the heatmap,
// when it is on) again, keeping the series on screen until the answer
// arrives: no "Loading…" in between. Without a database there is nothing
// to do.
// SetProjects replaces the project definitions, forgets the colours (the
// shared Colors, so the activity view starts afresh too) and loads the
// window again, so a changed definition shows without a restart.
func (m Model) SetProjects(r project.Resolver) (tea.Model, tea.Cmd) {
	m.opts.Projects = r
	m.colors.Reset()
	return m.Reload()
}

func (m Model) Reload() (tea.Model, tea.Cmd) {
	if m.opts.Loader == nil {
		return m, nil
	}
	m.cache = map[filter]store.ActivitySeries{}
	m.heat.cache = map[heatFilter]store.Stats{}
	m.seq++
	m.stop()
	m.err = nil
	m, cmd := m.fetch(m.filter(), true)
	var heatCmd tea.Cmd
	if m.heat.on {
		m.heat.seq++
		m.stopHeat()
		m.heat.err = nil
		m, heatCmd = m.fetchHeat(m.heatFilter(), true)
	}
	return m, tea.Batch(cmd, heatCmd)
}

// fetch starts the query for f with the current sequence number. A quiet
// fetch (reload) does not announce itself in the status line.
func (m Model) fetch(f filter, quiet bool) (Model, tea.Cmd) {
	m.loading = !quiet
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	seq, now, l := m.seq, m.opts.Now, m.opts.Loader
	af := store.ActivityFilter{
		Since: m.window.Since(), Until: m.window.Until(), Location: m.opts.Location,
		Period: m.window.Period, Dimension: m.dim, TopN: TopN, Project: m.opts.Projects.Key,
	}
	return m, func() tea.Msg {
		start := now()
		s, err := l.ActivitySeries(ctx, af)
		return resultMsg{seq: seq, filter: f, series: s, took: now().Sub(start), err: err}
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

// heatFilter is the heatmap cache key of the current window.
func (m Model) heatFilter() heatFilter {
	return heatFilter{since: m.window.Since().Unix(), until: m.window.Until().Unix()}
}

// loadHeat shows the cached stats of the current window, or starts loading
// them. A running heatmap load is cancelled; its result would be stale.
func (m Model) loadHeat() (Model, tea.Cmd) {
	m.heat.seq++
	m.stopHeat()
	m.heat.err = nil
	if m.opts.Loader == nil {
		return m, nil
	}
	f := m.heatFilter()
	if s, ok := m.heat.cache[f]; ok {
		m.heat.stats, m.heat.loaded = s, true
		return m, nil
	}
	m.heat.loaded = false
	return m.fetchHeat(f, false)
}

// fetchHeat starts the heatmap query for f with the current heatmap
// sequence number; quiet as in fetch.
func (m Model) fetchHeat(f heatFilter, quiet bool) (Model, tea.Cmd) {
	m.heat.loading = !quiet
	ctx, cancel := context.WithCancel(context.Background())
	m.heat.cancel = cancel
	seq, l := m.heat.seq, m.opts.Loader
	sf := store.StatsFilter{Since: m.window.Since(), Until: m.window.Until(), Location: m.opts.Location}
	return m, func() tea.Msg {
		s, err := l.Stats(ctx, sf)
		return heatResultMsg{seq: seq, filter: f, stats: s, err: err}
	}
}

// stopHeat cancels the running heatmap load, if any.
func (m *Model) stopHeat() {
	if m.heat.cancel != nil {
		m.heat.cancel()
		m.heat.cancel = nil
	}
	m.heat.loading = false
}

// heatResult caches a successful answer and shows it when it is still
// current.
func (m Model) heatResult(msg heatResultMsg) Model {
	if msg.err == nil {
		m.heat.cache[msg.filter] = msg.stats
	}
	if msg.seq != m.heat.seq {
		return m
	}
	m.heat.loading, m.heat.cancel = false, nil
	m.heat.err = msg.err
	if msg.err != nil {
		return m
	}
	m.heat.stats, m.heat.loaded = msg.stats, true
	return m
}

// result caches a successful answer and shows it when it is still current.
func (m Model) result(msg resultMsg) (Model, tea.Cmd) {
	if msg.err == nil {
		m.cache[msg.filter] = msg.series
	}
	if msg.seq != m.seq {
		return m, nil
	}
	m.loading, m.cancel = false, nil
	m.took, m.err = msg.took, msg.err
	if msg.err != nil {
		return m, nil
	}
	m.series, m.loaded = msg.series, true
	return m, m.rebuild()
}

// rebuild derives the spec from the series and the metric and hands it to
// the chart, returning the chart's render command. Colours are assigned
// once per key and kept across switches; a configured project keeps its
// configured colour.
func (m *Model) rebuild() tea.Cmd {
	if !m.loaded {
		return nil
	}
	names := m.opts.Projects.Names(m.series.Keys, m.series.Dirs, m.opts.Home)
	if m.dim == store.DimProject {
		for _, k := range m.series.Keys {
			if hex := m.opts.Projects.Color(k); hex != "" {
				m.colors.Fix(k, lipgloss.Color(hex))
			}
		}
	}
	m.spec = m.colors.Assign(chart.FromSeries(m.series, m.window.Period, m.metric, names))
	m.spec.Marks = chart.DayMarks(m.series, m.window.Period, m.opts.Holidays)
	return tea.Batch(m.chart.SetSpec(m.spec), m.chart.SetSize(m.chartSize()))
}

// split is how the body rows are shared: the tiles row (0 or 1), the
// chart, the heatmap and the table.
type split struct {
	tiles, chart, heat, table int
}

// layout splits the body height between the tiles, the chart, the heatmap
// and the table: the heatmap, when on, takes the height of the largest
// layout that fits while the chart keeps MinChartHeight beside it (see
// chart.HeatmapLayoutFor); the table gets one line per series plus its
// header, but shrinks before the chart goes below MinChartHeight; the
// tiles take one row when the chart keeps MinChartHeight beside the full
// table; the chart gets the rest.
func (m Model) layout() split {
	var l split
	avail := max(m.height-chromeRows, 0)
	if m.heat.on {
		l.heat = chart.HeatmapLayoutFor(max(m.width-1, 0), avail-MinChartHeight).Height()
		avail -= l.heat
	}
	want := 0
	if m.loaded && len(m.spec.Series) > 0 {
		want = len(m.spec.Series) + 1
	}
	l.table = min(want, max(avail-MinChartHeight, 0))
	if want > 0 && l.table == want && avail-want-1 >= MinChartHeight {
		l.tiles = 1
	}
	l.chart = avail - l.table - l.tiles
	return l
}

// chartSize is the chart area: the body width less the indent, and the
// chart rows of the layout.
func (m Model) chartSize() (w, h int) {
	return max(m.width-1, 0), m.layout().chart
}

// ShortHelp implements help.KeyMap.
func (m Model) ShortHelp() []key.Binding { return m.keys.ShortHelp() }

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// View implements tea.Model: the switches, the tiles, the chart, a rule,
// the table and the status line.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	l := m.layout()
	block := func(s string, h int) string {
		return lipgloss.NewStyle().Width(m.width).Height(h).MaxWidth(m.width).MaxHeight(h).Render(s)
	}
	switches, _, _ := m.switches()
	parts := []string{switches}
	if l.tiles > 0 {
		parts = append(parts, block(m.tiles(), l.tiles))
	}
	if l.chart > 0 {
		parts = append(parts, block(m.chartView(l.chart), l.chart))
	}
	if l.heat > 0 {
		parts = append(parts, block(m.heatView(l.heat), l.heat))
	}
	parts = append(parts, m.th.Rule.Render(strings.Repeat("─", m.width)))
	if l.table > 0 {
		parts = append(parts, block(indent(newTable(m.th, m.spec, m.series, m.window.Period, m.dim, m.width-1).View()), l.table))
	}
	parts = append(parts, " "+ansi.Truncate(m.status(), max(m.width-1, 0), "…"))
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// Names and values of the switches, in the order shown.
var (
	periodNames     = []string{"day", "week", "month"}
	periodValues    = []store.Period{store.PeriodDay, store.PeriodWeek, store.PeriodMonth}
	dimensionNames  = []string{"project", "agent", "directory"}
	dimensionValues = []store.Dimension{store.DimProject, store.DimAgent, store.DimDirectory}
)

// switches is the top line: period and dimension with the active value in
// brackets on the accent background, and the window on the right. It
// returns the columns of every period and dimension name, clipped like the
// line, so a click lands on what is shown.
func (m Model) switches() (string, []zone.Span, []zone.Span) {
	left := " period: "
	periods, pSpans := zone.Toggles(m.th.Active, periodNames, slices.Index(periodValues, m.window.Period), lipgloss.Width(left))
	left += periods + "  by: "
	dims, dSpans := zone.Toggles(m.th.Active, dimensionNames, slices.Index(dimensionValues, m.dim), lipgloss.Width(left))
	left += dims
	right := m.window.String()
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	line := left + strings.Repeat(" ", gap) + right
	if lipgloss.Width(line) > m.width {
		line = ansi.Truncate(line, m.width, "…")
		pSpans, dSpans = zone.Clip(pSpans, max(m.width-1, 0)), zone.Clip(dSpans, max(m.width-1, 0))
	}
	return line, pSpans, dSpans
}

// tiles is the row of key figures of the window above the chart: the
// events (and how many of them human and agent), the distinct sessions
// and the periods with activity, each a bold figure behind an accent bar
// with a muted label, so they read as tiles without a border. Empty until
// the series is loaded.
func (m Model) tiles() string {
	if !m.loaded {
		return ""
	}
	human, agent, active := 0, 0, 0
	for _, b := range m.series.Buckets {
		sum := 0
		for _, c := range b.Cells {
			human += c.Human
			agent += c.Agent
			sum += c.Human + c.Agent
		}
		if sum > 0 {
			active++
		}
	}
	tile := func(st lipgloss.Style, figure, label string) string {
		return m.th.AccentText.Render("▎") + st.Render(figure) + " " + m.th.Muted.Render(label)
	}
	parts := []string{
		tile(m.th.Figure, group(human+agent), "events"),
		tile(m.th.Actor(model.ActorHuman), group(human), "human"),
		tile(m.th.Actor(model.ActorAgent), group(agent), "agent"),
		tile(m.th.Figure, group(m.series.Sessions), "sessions"),
		tile(m.th.Figure, fmt.Sprintf("%d/%d", active, len(m.series.Buckets)), m.window.Period.String()+"s active"),
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), m.width, "…")
}

// chartView is the chart area: the container's view, or the hint while
// there is nothing to draw.
func (m Model) chartView(h int) string {
	if hint := m.hint(); hint != "" {
		return " " + m.th.Muted.Render(hint)
	}
	return indent(m.chart.View())
}

// hint is shown in the chart area instead of a chart.
func (m Model) hint() string {
	switch {
	case m.opts.Loader == nil:
		return "No database open."
	case m.err != nil:
		return "The query failed; see the status line."
	case !m.loaded:
		return "Loading…"
	case len(m.series.Keys) == 0:
		return "No activity between " + m.window.String() + "."
	}
	return ""
}

// heatView is the heatmap area of h rows: the rendered heatmap in the
// layout for that height, or the hint while there is nothing to draw.
func (m Model) heatView(h int) string {
	switch {
	case m.heat.err != nil:
		return " " + m.th.Muted.Render("The heatmap query failed; see the status line.")
	case !m.heat.loaded:
		return " " + m.th.Muted.Render("Loading heatmap…")
	}
	return indent(chart.RenderHeatmap(m.heat.stats, m.metric, max(m.width-1, 0), h, chart.HeatScaleFor(m.th.Dark)))
}

// status is the bottom line: the chart mode, the state of the load and
// its duration, and the state of the heatmap when it is on.
func (m Model) status() string {
	mode := m.chart.Mode()
	switch {
	case m.opts.Loader == nil:
		return mode + " · no database"
	case m.err != nil:
		return mode + " · " + m.th.Error.Render("error: "+m.err.Error())
	case m.loading:
		return mode + " · loading…"
	case !m.loaded:
		return mode
	}
	n := len(m.series.Buckets)
	return fmt.Sprintf("%s · %d %ss · %d ms%s", mode, n, m.window.Period, m.took.Milliseconds(), m.heatStatus())
}

// heatStatus is the heatmap's part of the status line: nothing while it is
// off, otherwise its state, including that the body is too low for it.
func (m Model) heatStatus() string {
	if !m.heat.on {
		return ""
	}
	switch {
	case m.heat.err != nil:
		return " · " + m.th.Error.Render("heatmap error: "+m.heat.err.Error())
	case m.layout().heat == 0:
		return " · heatmap needs more rows"
	case m.heat.loading:
		return " · loading heatmap…"
	}
	return " · heatmap"
}

// indent prefixes every line of s with a space.
func indent(s string) string {
	if s == "" {
		return ""
	}
	return " " + strings.ReplaceAll(s, "\n", "\n ")
}
