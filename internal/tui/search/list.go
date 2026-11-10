package search

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/viewer"
)

// Sentinels the store wraps around matches in snippets; renderSnippet turns
// them into a style. Control characters cannot occur in sanitised text.
const (
	hlStart = "\x1e"
	hlEnd   = "\x1f"
)

// Rows of the body around the two columns: input, rule, status.
const chromeRows = 3

// linesPerHit is the height of one hit: head line and snippet line. A
// group header (more than one type selected) is one line.
const (
	linesPerHit    = 2
	linesPerHeader = 1
)

// row is one entry of the list: the header of a group or a hit (an index
// into Model.hits).
type row struct {
	header bool
	group  int // index into Model.groups
	hit    int
}

func (r row) height() int {
	if r.header {
		return linesPerHeader
	}
	return linesPerHit
}

// buildRows lays the groups out as rows: the hits in group order, and with
// more than one type selected a header before every group, also an empty
// one, so a type without hits is visible.
func (m *Model) buildRows() {
	m.rows = m.rows[:0]
	n := 0
	for gi, g := range m.groups {
		if len(m.types) > 1 {
			m.rows = append(m.rows, row{header: true, group: gi})
		}
		for range g.Hits {
			m.rows = append(m.rows, row{group: gi, hit: n})
			n++
		}
	}
}

// rowOf returns the index of the row of hit i, -1 when there is none.
func (m Model) rowOf(i int) int {
	for r, rw := range m.rows {
		if !rw.header && rw.hit == i {
			return r
		}
	}
	return -1
}

// bodyRows is the height of the list and preview columns.
func (m Model) bodyRows() int { return max(m.height-chromeRows, 0) }

// View implements tea.Model: the input with the filters on its right, a
// rule with the type switcher and the project pin, the hit list beside
// the preview column, and the status line (or the repo filter being
// typed).
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	left, right := m.columns()
	rows := m.bodyRows()
	col := func(s string, w int) string {
		return lipgloss.NewStyle().Width(w).Height(rows).MaxWidth(w).MaxHeight(rows).Render(s)
	}
	sep := m.th.Rule.Render(strings.TrimSuffix(strings.Repeat("│\n", rows), "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, col(m.list(left, rows), left), sep, col(m.previewView(right, rows), right))
	rule, _ := m.ruleRow(left, right)
	bottom := " " + m.status()
	if m.focus == focusRepo {
		bottom = " " + m.repoIn.View()
	}
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		m.inputLine(), rule, body, bottom))
	switch m.focus {
	case focusInput:
		if c := m.input.Cursor(); c != nil {
			c.X++ // the space before the prompt
			v.Cursor = c
		}
	case focusRepo:
		if c := m.repoIn.Cursor(); c != nil {
			c.X++
			c.Y = m.height - 1
			v.Cursor = c
		}
	}
	return v
}

// inputLine is the search input with the filter label right-aligned.
func (m Model) inputLine() string {
	left := " " + m.input.View()
	label := m.th.Muted.Render(m.filters.label())
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(label)-1, 1)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+label+" ", m.width, "")
}

// columns splits the width into the list and the preview column, minus the
// separator.
func (m Model) columns() (left, right int) {
	left = (m.width - 1) / 2
	return left, max(m.width-left-1, 0)
}

// rowAt returns the index of the row that starts at or covers line y of
// the list column, -1 below the last row.
func (m Model) rowAt(y int) int {
	line := 0
	for r := m.offset; r < len(m.rows); r++ {
		line += m.rows[r].height()
		if y < line {
			return r
		}
	}
	return -1
}

// visibleRows is how many rows fit into the list column from row first;
// a row that would be cut off does not count.
func (m Model) visibleRows(first int) int {
	avail, n := m.bodyRows(), 0
	for r := first; r < len(m.rows); r++ {
		if avail -= m.rows[r].height(); avail < 0 {
			break
		}
		n++
	}
	return n
}

// list renders the visible rows in width w, or the hint, or the project
// choice.
func (m Model) list(w, rows int) string {
	if m.focus == focusPick {
		return m.pickView(w, rows)
	}
	if h := m.hint(); h != "" {
		return " " + m.th.Muted.Render(ansi.Truncate(h, max(w-1, 0), "…"))
	}
	n := m.visibleRows(m.offset)
	lines := make([]string, 0, rows)
	for r := m.offset; r < len(m.rows) && r < m.offset+n; r++ {
		rw := m.rows[r]
		if rw.header {
			lines = append(lines, m.header(m.groups[rw.group], w))
			continue
		}
		lines = append(lines, m.entry(rw.hit, w)...)
	}
	return strings.Join(lines, "\n")
}

// header renders a group header: the type in the title style and the
// number of hits, "N of TOTAL" when the limit cut hits off.
func (m Model) header(g store.TypeHits, w int) string {
	count := fmt.Sprintf("%d", len(g.Hits))
	if g.Total > len(g.Hits) {
		count = fmt.Sprintf("%d of %d, limit reached", len(g.Hits), g.Total)
	}
	return ansi.Truncate(" "+m.th.Title.Render(string(g.Type))+m.th.Muted.Render("  "+count), w, "…")
}

// entry renders hit i as two lines in the format of its type.
func (m Model) entry(i, w int) []string {
	h := m.hits[i]
	mark, base := " ", lipgloss.NewStyle()
	if i == m.cursor {
		mark = "▸"
		if m.focus == focusList {
			base = m.th.Selected
		}
	}
	switch h.Type {
	case store.TypeSession:
		return m.entrySession(h, mark, base, w)
	case store.TypeMemory:
		return m.entryMemory(h, mark, base, w)
	}
	return m.entryEvent(h, mark, base, w)
}

// entryEvent renders an event hit: a mark, time (muted), session name
// and actor (in the actor's colour), then the snippet with the matches
// highlighted. The marked hit always carries the mark (enter opens it
// from the input too, and the preview shows it); its head line is drawn
// on the selection background only while the list has the focus.
func (m Model) entryEvent(h store.Hit, mark string, base lipgloss.Style, w int) []string {
	ev := h.Event
	ts := h.TS.In(m.opts.Location).Format("2006-01-02 15:04")
	actor := string(ev.Event.Actor)
	// An event with attachments carries the mark and their number after
	// the time, where a narrow column still shows it; the name gives up
	// the room for it first.
	tail := ""
	if n := len(m.attached[ev.Event.ID]); n > 0 {
		tail = " " + viewer.AttachmentMark + strconv.Itoa(n)
	}
	nameWidth := w - 1 - len(ts) - 2 - 2 - ansi.StringWidth(actor) - ansi.StringWidth(tail)
	name := ansi.Truncate(strings.TrimSpace(ev.SessionName), max(nameWidth, 0), "…")
	pad := strings.Repeat(" ", max(nameWidth-ansi.StringWidth(name), 0))
	// Every piece is styled on its own, so the selection background spans
	// the row while time and actor keep their colours.
	if tail != "" {
		tail = m.th.Mark.Inherit(base).Render(tail)
	}
	head := m.th.Mark.Inherit(base).Render(mark) + m.th.Muted.Inherit(base).Render(ts) + tail +
		base.Render("  "+name+pad+"  ") + m.th.Actor(model.Actor(actor)).Inherit(base).Render(actor)
	head = ansi.Truncate(head, w, "")
	return []string{head, "  " + m.renderSnippet(h.Snippet, max(w-2, 0))}
}

// entrySession renders a session hit: a mark, the start time (muted) and
// the name that matched with the words highlighted, then the size and the
// project, muted.
func (m Model) entrySession(h store.Hit, mark string, base lipgloss.Style, w int) []string {
	s := h.Session
	ts := h.TS.In(m.opts.Location).Format("2006-01-02 15:04")
	room := max(w-1-len(ts)-2, 0)
	head := m.th.Mark.Inherit(base).Render(mark) + m.th.Muted.Inherit(base).Render(ts) +
		base.Render("  ") + m.renderSnippet(h.Snippet, room)
	info := fmt.Sprintf("%d events", s.Events)
	if p := m.projectName(h); p != "" {
		info += "  " + p
	}
	return []string{ansi.Truncate(head, w, ""), "  " + m.th.Muted.Render(ansi.Truncate(info, max(w-2, 0), "…"))}
}

// entryMemory renders a memory hit: a mark, the time of the version
// (muted), project and file path, then the snippet of the file.
func (m Model) entryMemory(h store.Hit, mark string, base lipgloss.Style, w int) []string {
	ts := h.TS.In(m.opts.Location).Format("2006-01-02 15:04")
	head := m.th.Mark.Inherit(base).Render(mark) + m.th.Muted.Inherit(base).Render(ts) +
		base.Render("  ") + m.th.Title.Inherit(base).Render(m.projectName(h)) + base.Render("  "+h.Memory.Path)
	return []string{ansi.Truncate(head, w, ""), "  " + m.renderSnippet(markdownMarks.Replace(h.Snippet), max(w-2, 0))}
}

// markdownMarks drops the emphasis and code markers of a memory file's
// snippet, as the preview does.
var markdownMarks = strings.NewReplacer("**", "", "`", "")

// renderSnippet collapses whitespace, styles the marked matches and cuts the
// snippet to width w.
func (m Model) renderSnippet(s string, w int) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for i, part := range strings.Split(s, hlStart) {
		if i == 0 {
			b.WriteString(part)
			continue
		}
		match, rest, _ := strings.Cut(part, hlEnd)
		b.WriteString(m.th.Match.Render(match))
		b.WriteString(rest)
	}
	return ansi.Truncate(b.String(), w, "…")
}
