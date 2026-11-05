package activity

import (
	"context"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// projects is the state of the project colouring of the bars (key p): the
// bars show which project was worked on when, every project in the colour
// the stats view gives it (chart.Colors, shared by the views), with a
// legend below the rows. Its data is a store.ProjectDays of the window,
// loaded and cached per filter like the days, but only while the colouring
// is on. The data on screen stays until the answer for a new window
// arrives; a day it does not cover draws its plain bar meanwhile.
type projects struct {
	on      bool
	seq     uint64 // counts changes of the filter and the toggling
	cancel  context.CancelFunc
	cache   map[filter]store.ProjectDays
	loaded  bool // data holds the data of the current filter
	loading bool
	data    store.ProjectDays
	err     error
}

// projectsMsg is the answer of one project load; it is shown only when seq
// is still the current project sequence, and cached in any case.
type projectsMsg struct {
	seq    uint64
	filter filter
	data   store.ProjectDays
	err    error
}

// toggleProjects switches the project colouring on, loading its data, or
// off, cancelling a running load; the cache stays for the next time.
func (m Model) toggleProjects() (Model, tea.Cmd) {
	m.proj.on = !m.proj.on
	if m.proj.on {
		var cmd tea.Cmd
		m, cmd = m.loadProjects()
		m.clampOffset()
		return m, cmd
	}
	m.proj.seq++
	m.stopProjects()
	m.clampOffset()
	return m, nil
}

// loadProjects shows the cached project data of the current filter, or
// starts loading it. A running project load is cancelled; its result would
// be stale.
func (m Model) loadProjects() (Model, tea.Cmd) {
	m.proj.seq++
	m.stopProjects()
	m.proj.err = nil
	if m.opts.Loader == nil {
		return m, nil
	}
	f := m.filter()
	if d, ok := m.proj.cache[f]; ok {
		m.proj.data, m.proj.loaded = d, true
		return m, nil
	}
	m.proj.loaded = false
	return m.fetchProjects(f, false)
}

// reloadProjects is the project part of Reload: the cache is dropped and,
// while the colouring is on, the current window loaded again quietly.
func (m Model) reloadProjects() (Model, tea.Cmd) {
	m.proj.cache = map[filter]store.ProjectDays{}
	if !m.proj.on || m.opts.Loader == nil {
		return m, nil
	}
	m.proj.seq++
	m.stopProjects()
	m.proj.err = nil
	return m.fetchProjects(m.filter(), true)
}

// fetchProjects starts the project query for f with the current project
// sequence number; quiet as in fetch.
func (m Model) fetchProjects(f filter, quiet bool) (Model, tea.Cmd) {
	m.proj.loading = !quiet
	ctx, cancel := context.WithCancel(context.Background())
	m.proj.cancel = cancel
	seq, l := m.proj.seq, m.opts.Loader
	wf := store.WorkdayFilter{
		Since: m.since(), Until: m.until(), Location: m.opts.Location,
		DayStart: m.opts.DayStart, HumanOnly: m.humanOnly,
	}
	return m, func() tea.Msg {
		d, err := l.ProjectDays(ctx, wf)
		return projectsMsg{seq: seq, filter: f, data: d, err: err}
	}
}

// stopProjects cancels the running project load, if any.
func (m *Model) stopProjects() {
	if m.proj.cancel != nil {
		m.proj.cancel()
		m.proj.cancel = nil
	}
	m.proj.loading = false
}

// projectsResult caches a successful answer and shows it when it is still
// current.
func (m Model) projectsResult(msg projectsMsg) Model {
	if msg.err == nil {
		m.proj.cache[msg.filter] = msg.data
	}
	if msg.seq != m.proj.seq {
		return m
	}
	m.proj.loading, m.proj.cancel = false, nil
	m.proj.err = msg.err
	if msg.err != nil {
		return m
	}
	m.proj.data, m.proj.loaded = msg.data, true
	return m
}

// legendRows is the height of the legend while the colouring is on.
func (m Model) legendRows() int {
	if m.proj.on {
		return 1
	}
	return 0
}

// Glyphs of the project bars: one per palette colour in rank order, then
// the folded rest beyond the palette and the gap, so the bars read without
// colours too; the legend names every glyph.
var projectGlyphs = [...]rune{'█', '▓', '▒', '▞', '▚', '▌'}

const (
	otherGlyph = '▪'
	gapGlyph   = '░'
)

// projectLayout is what the rows need to draw the bars by project: the
// data by day and, per project of the window in rank order, its glyph and
// style. The projects beyond the palette are the folded rest in the muted
// colour of the stats view's "other".
type projectLayout struct {
	days   map[string]store.ProjectDay // by label
	glyphs []rune
	styles []lipgloss.Style
	gap    lipgloss.Style
	other  lipgloss.Style
}

// projectLayout builds the layout from the data on screen, stale or not;
// nil while the colouring is off. Colours come from the shared
// chart.Colors, toned for the background.
func (m Model) projectLayout() *projectLayout {
	if !m.proj.on {
		return nil
	}
	fg := func(key string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(m.th.Tone(m.opts.Colors.Color(key)))
	}
	p := &projectLayout{days: map[string]store.ProjectDay{}, gap: m.styles.faint, other: fg(store.OtherKey)}
	for _, d := range m.proj.data.Days {
		p.days[d.Label] = d
	}
	for i, k := range m.proj.data.Keys {
		if i < len(projectGlyphs) {
			p.glyphs = append(p.glyphs, projectGlyphs[i])
			p.styles = append(p.styles, fg(k))
		} else {
			p.glyphs = append(p.glyphs, otherGlyph)
			p.styles = append(p.styles, p.other)
		}
	}
	return p
}

// ranks returns, per cell of the bar of d, the index of the project with
// the most counted events in the cell's time range, -1 for a cell without
// events; a tie goes to the project with more events in the window. Nil
// when the data does not cover the day.
func (p *projectLayout) ranks(l layout, d store.Workday) []int {
	pd, ok := p.days[d.Label]
	if !ok {
		return nil
	}
	start := store.DayBoundary(d.Day, l.dayStart)
	total := store.DayBoundary(d.Day.AddDate(0, 0, 1), l.dayStart).Sub(start)
	counts := make([][]int, l.bar)
	for _, s := range pd.Slots {
		i := min(max(int(float64(s.Offset)/float64(total)*float64(l.bar)), 0), l.bar-1)
		if counts[i] == nil {
			counts[i] = make([]int, len(p.glyphs))
		}
		if s.Project < len(p.glyphs) {
			counts[i][s.Project] += s.Events
		}
	}
	out := make([]int, l.bar)
	for i := range out {
		out[i] = -1
		for j, n := range counts[i] {
			if n > 0 && (out[i] < 0 || n > counts[i][out[i]]) {
				out[i] = j
			}
		}
	}
	return out
}

// bar draws the bar of d by project: an active cell takes the glyph and
// colour of its project, an active cell without events the gap glyph, the
// rest stays the background (band and lines). A day the data does not
// cover draws its plain bar.
func (p *projectLayout) bar(l layout, cells []cellKind, d store.Workday) string {
	ranks := p.ranks(l, d)
	if ranks == nil {
		return l.renderBar(cells, l.caption(cells, d))
	}
	return l.renderCells(len(cells), func(i int) (int, rune, lipgloss.Style) {
		switch {
		case cells[i] < cellCore:
			return int(cells[i]), glyphs[cells[i]], l.styles.cell(cells[i])
		case ranks[i] >= 0:
			return int(cellHoliday) + 2 + ranks[i], p.glyphs[ranks[i]], p.styles[ranks[i]]
		}
		return int(cellHoliday) + 1, gapGlyph, p.gap
	}, l.caption(cells, d))
}

// legend is the line below the rows while the colouring is on: the
// projects of the window by volume, each with its glyph in its colour, its
// name as in the stats view and its counted events; the projects beyond
// the palette folded into other. The data on screen is described, stale
// or not; before the first answer the line says so.
func (m Model) legend() string {
	st := m.styles
	label := st.label.Render("projects:")
	switch {
	case m.proj.err != nil:
		return label + " " + m.th.Error.Render("error: "+m.proj.err.Error())
	case !m.proj.loaded && len(m.proj.data.Keys) == 0:
		if m.proj.loading {
			return label + " " + st.faint.Render("loading…")
		}
		return label + " " + st.faint.Render("no activity")
	}
	data, p := m.proj.data, m.projectLayout()
	names := project.Names(data.Keys, data.Dirs, m.opts.Home)
	var parts []string
	other := 0
	for i, k := range data.Keys {
		if i >= len(projectGlyphs) {
			other += data.Events[i]
			continue
		}
		parts = append(parts, p.styles[i].Render(string(p.glyphs[i]))+" "+names[k]+" "+st.faint.Render(strconv.Itoa(data.Events[i])))
	}
	if other > 0 {
		parts = append(parts, p.other.Render(string(otherGlyph))+" "+store.OtherKey+" "+st.faint.Render(strconv.Itoa(other)))
	}
	if len(parts) == 0 {
		return label + " " + st.faint.Render("no activity")
	}
	return label + " " + strings.Join(parts, "  ")
}
