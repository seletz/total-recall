package activity

import (
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/store"
)

// cellKind is what one cell of a day's time axis shows.
type cellKind int

// Cell kinds, background first, then the bar segments. A kind has one
// glyph and one style, so the bar reads without colours too.
const (
	cellEmpty   cellKind = iota // outside the core hours, no activity
	cellBand                    // within the core hours, no activity
	cellLine                    // start or end of the core hours, no activity
	cellCore                    // activity within the core hours
	cellOutside                 // activity before or after them, same calendar day
	cellNight                   // activity after midnight, before the day boundary
	cellWeekend                 // activity on a weekend
	cellHoliday                 // activity on a public holiday
)

// glyphs of the cell kinds, in cellKind order.
var glyphs = [...]rune{'·', '─', '│', '█', '▓', '▒', '█', '█'}

// cells classifies the bar cells of a day: the axis runs from the day
// boundary to the next one (24 hours, 23 or 25 on a DST change) and cell i
// covers the i-th fraction of it. The bar covers the cells from the first
// to the last activity, at least one; an active cell is classified by the
// instant of its middle, clamped to the activity, against the core hours
// and midnight. Without activity the cells show the core-hours band and
// the lines at its start and end; a weekend, a holiday and no working
// time have neither, and every active cell is cellWeekend, cellHoliday or
// cellCore.
func (l layout) cells(d store.Workday) []cellKind {
	out := make([]cellKind, l.bar)
	start := store.DayBoundary(d.Day, l.dayStart)
	total := store.DayBoundary(d.Day.AddDate(0, 0, 1), l.dayStart).Sub(start)
	at := func(i float64) time.Time { return start.Add(time.Duration(i / float64(l.bar) * float64(total))) }
	pos := func(t time.Time) float64 { return min(max(float64(t.Sub(start))/float64(total), 0), 1) }
	index := func(t time.Time) int { return min(int(pos(t)*float64(l.bar)), l.bar-1) }

	weekend := store.IsWeekend(d.Day)
	var coreStart, coreEnd time.Time
	if !l.work.IsZero() && !d.IsOff() {
		coreStart, coreEnd = l.work.Core(d.Day)
		for i := range out {
			mid := at(float64(i) + 0.5)
			if !mid.Before(coreStart) && mid.Before(coreEnd) {
				out[i] = cellBand
			}
		}
		for _, t := range []time.Time{coreStart, coreEnd} {
			if !t.Before(start) && t.Before(start.Add(total)) {
				out[index(t)] = cellLine
			}
		}
	}
	if !d.Active() {
		return out
	}
	a := index(d.First)
	b := min(max(int(math.Ceil(pos(d.Last)*float64(l.bar))), a+1), l.bar)
	midnight := store.DayBoundary(d.Day.AddDate(0, 0, 1), 0)
	for i := a; i < b; i++ {
		t := at(float64(i) + 0.5)
		t = time.Unix(0, min(max(t.UnixNano(), d.First.UnixNano()), d.Last.UnixNano()))
		switch {
		case d.IsHoliday():
			out[i] = cellHoliday
		case weekend:
			out[i] = cellWeekend
		case l.work.IsZero():
			out[i] = cellCore
		case !t.Before(midnight):
			out[i] = cellNight
		case t.Before(coreStart) || !t.Before(coreEnd):
			out[i] = cellOutside
		default:
			out[i] = cellCore
		}
	}
	return out
}

// caption is text drawn over the empty cells of a bar: the holiday name.
type caption struct {
	at   int    // first cell
	text []rune // one rune per cell; nil for none
}

// caption places the holiday name of d on the axis: one cell in from the
// left when the bar starts after it, else one cell after the bar when it
// fits there; nothing on an ordinary day or without room.
func (l layout) caption(cells []cellKind, d store.Workday) caption {
	if !d.IsHoliday() {
		return caption{}
	}
	text := []rune(d.Holiday)
	first, last := len(cells), -1
	for i, c := range cells {
		if c >= cellCore {
			first = min(first, i)
			last = i
		}
	}
	switch {
	case first >= 1+len(text)+1:
		return caption{at: 1, text: text}
	case last+2+len(text) <= len(cells):
		return caption{at: last + 2, text: text}
	}
	return caption{}
}

// renderBar draws the cells, one styled run per stretch of equal kind,
// with the caption in the holiday colour over the cells it covers.
func (l layout) renderBar(cells []cellKind, c caption) string {
	return l.renderCells(len(cells), func(i int) (int, rune, lipgloss.Style) {
		return int(cells[i]), glyphs[cells[i]], l.styles.cell(cells[i])
	}, c)
}

// renderCells draws n cells, one styled run per stretch of cells with the
// same id, with the caption in the holiday colour over the cells it
// covers. cell returns the id, the glyph and the style of a cell; cells
// with the same id have the same glyph and style.
func (l layout) renderCells(n int, cell func(i int) (int, rune, lipgloss.Style), c caption) string {
	var b strings.Builder
	for i := 0; i < n; {
		if i >= c.at && i < c.at+len(c.text) {
			b.WriteString(l.styles.holidayName.Render(string(c.text[i-c.at:])))
			i = c.at + len(c.text)
			continue
		}
		id, glyph, style := cell(i)
		j := i + 1
		for j < n && (j < c.at || j >= c.at+len(c.text)) {
			if jd, _, _ := cell(j); jd != id {
				break
			}
			j++
		}
		b.WriteString(style.Render(strings.Repeat(string(glyph), j-i)))
		i = j
	}
	return b.String()
}
