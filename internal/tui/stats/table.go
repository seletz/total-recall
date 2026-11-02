package stats

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// Widths of the number columns; the name column takes the rest.
const (
	sessionsWidth = 10
	periodsWidth  = 14 // "session-months"
	eventsWidth   = 9
	shareWidth    = 7
	columnGap     = 2
)

// sessionPeriodsTitle names the column of sessions summed over the
// periods: session-days, session-weeks or session-months.
func sessionPeriodsTitle(p store.Period) string {
	return "session-" + p.String() + "s"
}

// newTable returns the table of a series: one row per series of spec (by
// volume, "other" last) with the distinct sessions of the window, the
// sessions summed over the periods (session-days, -weeks or -months: a
// session active in three periods counts three), human and agent events
// and the share of all events in the window; the header is in the label
// style of th. The table is not interactive: no cursor, no key map.
func newTable(th theme.Theme, spec chart.Spec, series store.ActivitySeries, period store.Period, dim store.Dimension, width int) table.Model {
	total := 0
	rows := make([][4]int, len(spec.Series))
	for j := range spec.Series {
		if j < len(series.KeySessions) {
			rows[j][0] = series.KeySessions[j]
		}
	}
	for _, b := range series.Buckets {
		for j := range spec.Series {
			if j < len(b.Cells) {
				a := b.Cells[j].Activity
				rows[j][1] += a.Sessions
				rows[j][2] += a.Human
				rows[j][3] += a.Agent
				total += a.Human + a.Agent
			}
		}
	}
	trows := make([]table.Row, len(spec.Series))
	for j, s := range spec.Series {
		share := ""
		if total > 0 {
			share = fmt.Sprintf("%d %%", (rows[j][2]+rows[j][3])*100/total)
		}
		trows[j] = table.Row{
			s.Name,
			right(group(rows[j][0]), sessionsWidth),
			right(group(rows[j][1]), periodsWidth),
			right(group(rows[j][2]), eventsWidth),
			right(group(rows[j][3]), eventsWidth),
			right(share, shareWidth),
		}
	}
	nameWidth := max(width-sessionsWidth-periodsWidth-2*eventsWidth-shareWidth-5*columnGap, 1)
	cols := []table.Column{
		{Title: dim.String(), Width: nameWidth},
		{Title: right("sessions", sessionsWidth), Width: sessionsWidth},
		{Title: right(sessionPeriodsTitle(period), periodsWidth), Width: periodsWidth},
		{Title: right("human", eventsWidth), Width: eventsWidth},
		{Title: right("agent", eventsWidth), Width: eventsWidth},
		{Title: right("share", shareWidth), Width: shareWidth},
	}
	t := table.New(table.WithColumns(cols), table.WithRows(trows), table.WithWidth(width), table.WithHeight(len(trows)+1))
	t.SetStyles(table.Styles{
		Header:   th.Label.PaddingRight(columnGap),
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
