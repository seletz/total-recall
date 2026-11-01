package search

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Sentinels the store wraps around matches in snippets; renderSnippet turns
// them into a style. Control characters cannot occur in sanitised text.
const (
	hlStart = "\x1e"
	hlEnd   = "\x1f"
)

// Rows of the body around the two columns: input, rule, status.
const chromeRows = 3

// linesPerHit is the height of one entry: head line and snippet line.
const linesPerHit = 2

var (
	markStyle  = lipgloss.NewStyle().Reverse(true)
	hitStyle   = lipgloss.NewStyle().Bold(true).Underline(true)
	faintStyle = lipgloss.NewStyle().Faint(true)
)

// View implements tea.Model: the input with the filters on its right, a
// rule, the hit list beside the preview column, and the status line (or
// the repo filter being typed).
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	left, right := m.columns()
	rows := max(m.height-chromeRows, 0)
	col := func(s string, w int) string {
		return lipgloss.NewStyle().Width(w).Height(rows).MaxWidth(w).MaxHeight(rows).Render(s)
	}
	sep := strings.TrimSuffix(strings.Repeat("│\n", rows), "\n")
	body := lipgloss.JoinHorizontal(lipgloss.Top, col(m.list(left, rows), left), sep, col(m.previewView(right, rows), right))
	rule := strings.Repeat("─", left) + "┬" + strings.Repeat("─", right)
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
	label := faintStyle.Render(m.filters.label())
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(label)-1, 1)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+label+" ", m.width, "")
}

// columns splits the width into the list and the preview column, minus the
// separator.
func (m Model) columns() (left, right int) {
	left = (m.width - 1) / 2
	return left, max(m.width-left-1, 0)
}

// visibleHits is how many entries fit into the list column.
func (m Model) visibleHits() int {
	return max(m.height-chromeRows, 0) / linesPerHit
}

// list renders the visible hits in width w, or the hint.
func (m Model) list(w, rows int) string {
	if h := m.hint(); h != "" {
		return " " + faintStyle.Render(ansi.Truncate(h, max(w-1, 0), "…"))
	}
	n := m.visibleHits()
	lines := make([]string, 0, rows)
	for i := m.offset; i < len(m.hits) && i < m.offset+n; i++ {
		lines = append(lines, m.entry(i, w)...)
	}
	return strings.Join(lines, "\n")
}

// entry renders hit i as two lines: a mark, time, session name and actor,
// then the snippet. The marked hit is drawn in reverse video.
func (m Model) entry(i, w int) []string {
	h := m.hits[i]
	mark, style := " ", lipgloss.NewStyle()
	if i == m.cursor && m.focus == focusList {
		mark, style = "▸", markStyle
	}
	ts := h.Event.TS.In(m.opts.Location).Format("2006-01-02 15:04")
	actor := string(h.Event.Actor)
	nameWidth := w - 1 - len(ts) - 2 - 2 - ansi.StringWidth(actor)
	name := ansi.Truncate(strings.TrimSpace(h.SessionName), max(nameWidth, 0), "…")
	pad := strings.Repeat(" ", max(nameWidth-ansi.StringWidth(name), 0))
	head := mark + ts + "  " + name + pad + "  " + actor
	head = ansi.Truncate(head, w, "")
	return []string{style.Render(head), "  " + renderSnippet(h.Snippet, max(w-2, 0))}
}

// renderSnippet collapses whitespace, styles the marked matches and cuts the
// snippet to width w.
func renderSnippet(s string, w int) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for i, part := range strings.Split(s, hlStart) {
		if i == 0 {
			b.WriteString(part)
			continue
		}
		match, rest, _ := strings.Cut(part, hlEnd)
		b.WriteString(hitStyle.Render(match))
		b.WriteString(rest)
	}
	return ansi.Truncate(b.String(), w, "…")
}
