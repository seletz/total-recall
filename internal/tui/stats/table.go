package stats

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
)

// Widths of the number columns; the name column takes the rest.
const (
	sessionsWidth = 10
	eventsWidth   = 9
	shareWidth    = 7
	columnGap     = 2
)

// newTable returns the table of a series: one row per series of spec (by
// volume, "other" last) with sessions, human and agent events and the share
// of all events in the window. Sessions are summed over the periods, so a
// session active in several periods counts once per period; the store has
// no distinct count over the whole window. The table is not interactive:
// no cursor, no key map.
func newTable(spec chart.Spec, series store.ActivitySeries, dim store.Dimension, width int) table.Model {
	total := 0
	rows := make([][3]int, len(spec.Series))
	for _, b := range series.Buckets {
		for j := range spec.Series {
			if j < len(b.Cells) {
				a := b.Cells[j].Activity
				rows[j][0] += a.Sessions
				rows[j][1] += a.Human
				rows[j][2] += a.Agent
				total += a.Human + a.Agent
			}
		}
	}
	trows := make([]table.Row, len(spec.Series))
	for j, s := range spec.Series {
		share := ""
		if total > 0 {
			share = fmt.Sprintf("%d %%", (rows[j][1]+rows[j][2])*100/total)
		}
		trows[j] = table.Row{
			s.Name,
			right(group(rows[j][0]), sessionsWidth),
			right(group(rows[j][1]), eventsWidth),
			right(group(rows[j][2]), eventsWidth),
			right(share, shareWidth),
		}
	}
	nameWidth := max(width-sessionsWidth-2*eventsWidth-shareWidth-4*columnGap, 1)
	cols := []table.Column{
		{Title: dim.String(), Width: nameWidth},
		{Title: right("sessions", sessionsWidth), Width: sessionsWidth},
		{Title: right("human", eventsWidth), Width: eventsWidth},
		{Title: right("agent", eventsWidth), Width: eventsWidth},
		{Title: right("share", shareWidth), Width: shareWidth},
	}
	t := table.New(table.WithColumns(cols), table.WithRows(trows), table.WithWidth(width), table.WithHeight(len(trows)+1))
	t.SetStyles(table.Styles{
		Header:   lipgloss.NewStyle().Bold(true).PaddingRight(columnGap),
		Cell:     lipgloss.NewStyle().PaddingRight(columnGap),
		Selected: lipgloss.NewStyle(),
	})
	return t
}

// right pads s on the left to width w.
func right(s string, w int) string {
	return strings.Repeat(" ", max(w-len(s), 0)) + s
}

// group formats n with a space between thousands, like 21 870.
func group(n int) string {
	s := fmt.Sprint(n)
	if n < 0 || len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
