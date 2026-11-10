package dayview

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/dayreport"
	"github.com/seletz/total-recall/internal/store"
)

// summary is the quiet line below the title: the numbers of stats
// --workday (first and last activity, span, work, breaks, balance), the
// estimate of --worktime and the events.
func (m Model) summary() string {
	r, loc := m.rep, m.loc()
	w := r.Workday
	line := fmt.Sprintf(" %s–%s · span %s · work %s · breaks %s", w.FirstClock(loc), w.LastClock(loc),
		store.FormatSpan(w.Span()), store.FormatSpan(w.BlockSum()), store.FormatSpan(w.BreakSum()))
	if r.HasDiff {
		line += fmt.Sprintf(" · %s vs %s", store.FormatDiff(r.Diff), store.FormatSpan(r.Target))
	}
	line += fmt.Sprintf(" · booked %s (%s)", store.FormatSpan(r.Worktime.Raw), store.FormatSpan(r.Worktime.Rounded))
	line += fmt.Sprintf(" · %d events", w.Human+w.Agent)
	return m.th.Muted.Render(line)
}

// selBlock is the block of the selected row, -1 without one.
func (m Model) selBlock() int {
	its := m.items()
	if m.cursor < 0 || m.cursor >= len(its) {
		return -1
	}
	return its[m.cursor].block
}

// strip draws the day from the first to the last activity as two lines of
// full blocks: every block in grey, the selected one in the glyphs and
// colours of its projects (shares by seconds), the pauses a faint dot;
// below it the clock times of both ends. Without room (fewer than 20 cells)
// it is left out.
func (m Model) strip(ps map[string]projectStyle) []row {
	r, w := m.rep, m.rep.Workday
	width := m.width - 4
	span := w.Span().Seconds()
	if width < 20 || span <= 0 {
		return nil
	}
	top := make([]string, width)
	bottom := make([]string, width)
	for i := range top {
		top[i] = " "
		bottom[i] = m.th.Muted.Render("·")
	}
	cell := func(t time.Time) int { return min(int(t.Sub(w.First).Seconds()/span*float64(width-1)), width-1) }
	sel := m.selBlock()
	for bi, b := range r.Blocks {
		from, to := cell(b.Start), cell(b.End)
		n := to - from + 1
		var cells []string
		if bi == sel && len(b.Projects) > 0 {
			cells = projectCells(b, ps, n)
		} else {
			cells = make([]string, n)
			for i := range cells {
				cells[i] = m.th.Muted.Render("█")
			}
		}
		copy(top[from:], cells)
		copy(bottom[from:], cells)
	}
	axis := []rune(strings.Repeat(" ", width))
	first, last := w.First.In(m.loc()).Format("15:04"), w.Last.In(m.loc()).Format("15:04")
	copy(axis, []rune(first))
	copy(axis[max(width-len(last), len(first)+1):], []rune(last))
	return []row{
		{text: "  " + strings.Join(top, ""), item: -1},
		{text: "  " + strings.Join(bottom, ""), item: -1},
		{text: m.th.Muted.Render("  " + string(axis)), item: -1},
		{item: -1},
	}
}

// projectCells splits n cells over the projects of a block by their
// seconds; a block without seconds is n cells of its first project.
func projectCells(b dayreport.Block, ps map[string]projectStyle, n int) []string {
	total := 0
	for _, p := range b.Projects {
		total += p.Secs
	}
	out := make([]string, 0, n)
	acc := 0
	for i, p := range b.Projects {
		var cnt int
		switch {
		case total == 0 && i == 0:
			cnt = n
		case total == 0:
			cnt = 0
		default:
			acc += p.Secs
			cnt = acc*n/total - len(out)
		}
		s := ps[p.Key]
		for j := 0; j < cnt && len(out) < n; j++ {
			out = append(out, s.style.Render(string(s.glyph)))
		}
	}
	last := ps[b.Projects[len(b.Projects)-1].Key]
	for len(out) < n {
		out = append(out, last.style.Render(string(last.glyph)))
	}
	return out
}

// blockRows lists the blocks as plain lines with the pauses between them;
// the sessions of an expanded block follow its line.
func (m Model) blockRows(ps map[string]projectStyle) []row {
	r, st, loc := m.rep, m.th, m.loc()
	w := r.Workday
	var rows []row
	idx := 0
	for i, b := range r.Blocks {
		if b.Pause > 0 {
			rows = append(rows, row{text: st.Muted.Render("      · " + store.FormatSpan(b.Pause)), item: -1})
		}
		span := w.Clock(b.Start, loc) + "–" + w.Clock(b.End, loc)
		if b.Single() {
			span = w.Clock(b.Start, loc) + "*"
		}
		var parts, selParts []string
		for _, p := range b.Projects {
			s := ps[p.Key]
			label := " " + p.Name + " " + store.FormatSpan(time.Duration(p.Secs)*time.Second)
			parts = append(parts, s.style.Render(string(s.glyph))+label)
			selParts = append(selParts, s.style.Background(st.Selected.GetBackground()).Render(string(s.glyph))+st.Selected.Render(label))
		}
		hint := fmt.Sprintf("%d sessions", len(b.Sessions))
		if len(b.Sessions) == 1 {
			hint = "1 session"
		}
		if m.expanded[i] {
			hint = ""
		}
		text := fmt.Sprintf(" %-2d %-13s %6s  %3d ev  ", i+1, span, store.FormatSpan(time.Duration(b.Secs)*time.Second), b.Events) + st.Muted.Render(pad(hint, 11))
		right := strings.Join(parts, " · ")
		gap := max(m.width-lipgloss.Width(text)-lipgloss.Width(right)-2, 2)
		r := row{text: text + strings.Repeat(" ", gap) + right, item: idx}
		if idx == m.cursor {
			// the marker keeps the colour of its project on the selected row
			left := st.Selected.Render("▸" + strings.TrimPrefix(ansi.Strip(text), " "))
			r.sel = left + st.Selected.Render(strings.Repeat(" ", gap)) + strings.Join(selParts, st.Selected.Render(" · "))
		}
		rows = append(rows, r)
		idx++
		if !m.expanded[i] {
			continue
		}
		for _, s := range b.Sessions {
			title := s.Title
			if title == "" {
				title = s.SessionID
			}
			line := fmt.Sprintf("       %s–%s  %s", w.Clock(s.Start, loc), w.Clock(s.End, loc), title)
			line += "  " + st.Muted.Render(fmt.Sprintf("%s · h%d a%d", s.ProjectName, s.Human, s.Agent))
			rows = append(rows, row{text: line, item: idx})
			idx++
		}
	}
	return rows
}
