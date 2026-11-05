package activity

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/store"
)

// projects is the state of the project colouring of the bars (key p): the
// bars show which project was worked on when, every project in the colour
// the stats view gives it (chart.Colors, shared by the views), with a
// legend below the rows. Its data is a store.ProjectDay per day, fetched
// by the same query as the days while the colouring is on, cached one by
// one like them and part of the shown snapshot: the screen changes only
// when the days and, with the colouring on, the project days of the
// wanted window are all at hand, so scrolling does not flicker.
type projects struct {
	on    bool // the colouring wanted; shown.byProject once the data is at hand
	cache map[dayKey]store.ProjectDay
}

// toggleProjects switches the project colouring on or off: off shows the
// cached days at once, on shows the window as soon as its project days
// are at hand, loading them with the days when they are not.
func (m Model) toggleProjects() (Model, tea.Cmd) {
	m.proj.on = !m.proj.on
	return m.load()
}

// Glyphs of the project bars: one per palette colour in rank order, then
// the folded rest beyond the palette and the gap, so the bars read without
// colours too; the legend names every glyph.
var projectGlyphs = [...]rune{'█', '▓', '▒', '▞', '▚', '▌'}

const (
	otherGlyph = '▪'
	gapGlyph   = '░'
)

// projectLayout is what the rows and the legend need to draw the shown
// window and its lead by project: the projects of the window by counted
// events, each with its rank, glyph and style, and the project days of
// the window and the lead by label. The projects beyond the palette are
// the folded rest in the muted colour of the stats view's "other"; so is
// a project of the lead that the window does not have, under the rank
// after the last key.
type projectLayout struct {
	keys   []string // by counted events over the window, descending, then by key
	events []int    // per key
	dirs   []string // keys that are directories without a repository
	rank   map[string]int
	days   map[string]store.ProjectDay // by label
	glyphs []rune                      // per rank, then the rest
	styles []lipgloss.Style            // per rank, then the rest
	gap    lipgloss.Style
	other  lipgloss.Style
}

// projectLayout builds the layout from the shown snapshot; nil while it is
// not by project. Colours come from the shared chart.Colors, toned for the
// background.
func (m Model) projectLayout() *projectLayout {
	if !m.shown.byProject {
		return nil
	}
	fg := func(key string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(m.th.Tone(m.opts.Colors.Color(key)))
	}
	p := &projectLayout{rank: map[string]int{}, days: map[string]store.ProjectDay{}, gap: m.styles.faint, other: fg(store.OtherKey)}
	totals := map[string]int{}
	isDir := map[string]bool{}
	for _, d := range m.shown.beforeProjects {
		p.days[d.Label] = d
	}
	for _, d := range m.shown.projects {
		p.days[d.Label] = d
		for _, pr := range d.Projects {
			totals[pr.Key] += pr.Events
			if pr.Dir {
				isDir[pr.Key] = true
			}
		}
	}
	for k := range totals {
		p.keys = append(p.keys, k)
	}
	sort.Slice(p.keys, func(i, j int) bool {
		if totals[p.keys[i]] != totals[p.keys[j]] {
			return totals[p.keys[i]] > totals[p.keys[j]]
		}
		return p.keys[i] < p.keys[j]
	})
	for i, k := range p.keys {
		p.rank[k] = i
		p.events = append(p.events, totals[k])
		if isDir[k] {
			p.dirs = append(p.dirs, k)
		}
		if i < len(projectGlyphs) {
			p.glyphs = append(p.glyphs, projectGlyphs[i])
			p.styles = append(p.styles, fg(k))
		} else {
			p.glyphs = append(p.glyphs, otherGlyph)
			p.styles = append(p.styles, p.other)
		}
	}
	p.glyphs = append(p.glyphs, otherGlyph)
	p.styles = append(p.styles, p.other)
	return p
}

// ranks returns, per cell of the bar of d, the rank of the project with
// the most counted events in the cell's time range, -1 for a cell without
// events; a tie goes to the project with more events in the window. A
// project without a rank (one of the lead the window does not have) is
// the rest. Nil when the snapshot has no project day for d.
func (p *projectLayout) ranks(l layout, d store.Workday) []int {
	pd, ok := p.days[d.Label]
	if !ok {
		return nil
	}
	start := store.DayBoundary(d.Day, l.dayStart)
	total := store.DayBoundary(d.Day.AddDate(0, 0, 1), l.dayStart).Sub(start)
	counts := make([][]int, l.bar)
	for _, pr := range pd.Projects {
		r, ok := p.rank[pr.Key]
		if !ok {
			r = len(p.keys)
		}
		for _, s := range pr.Slots {
			i := min(max(int(float64(s.Offset)/float64(total)*float64(l.bar)), 0), l.bar-1)
			if counts[i] == nil {
				counts[i] = make([]int, len(p.keys)+1)
			}
			counts[i][r] += s.Events
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
// rest stays the background (band and lines). A day without project data
// draws its plain bar.
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

// legend is the line below the rows while the shown window is by project:
// its projects by volume, each with its glyph in its colour, its name as
// in the stats view (a configured project by its name) and its counted
// events; the projects beyond the palette folded into other.
func (m Model) legend() string {
	st := m.styles
	label := st.label.Render("projects:")
	p := m.projectLayout()
	if p == nil || len(p.keys) == 0 {
		return label + " " + st.faint.Render("no activity")
	}
	names := m.opts.Projects.Names(p.keys, p.dirs, m.opts.Home)
	var parts []string
	other := 0
	for i, k := range p.keys {
		if i >= len(projectGlyphs) {
			other += p.events[i]
			continue
		}
		parts = append(parts, p.styles[i].Render(string(p.glyphs[i]))+" "+names[k]+" "+st.faint.Render(strconv.Itoa(p.events[i])))
	}
	if other > 0 {
		parts = append(parts, p.other.Render(string(otherGlyph))+" "+store.OtherKey+" "+st.faint.Render(strconv.Itoa(other)))
	}
	return label + " " + strings.Join(parts, "  ")
}
