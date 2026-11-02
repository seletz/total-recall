package activity

import (
	"math"
	"strings"
	"time"

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
)

// glyphs of the cell kinds, in cellKind order.
var glyphs = [...]rune{'·', '─', '│', '█', '▓', '▒', '█'}

// cells classifies the bar cells of a day: the axis runs from the day
// boundary to the next one (24 hours, 23 or 25 on a DST change) and cell i
// covers the i-th fraction of it. The bar covers the cells from the first
// to the last activity, at least one; an active cell is classified by the
// instant of its middle, clamped to the activity, against the core hours
// and midnight. Without activity the cells show the core-hours band and
// the lines at its start and end; without working time there is neither,
// and every active cell is cellCore.
func (l layout) cells(d store.Workday) []cellKind {
	out := make([]cellKind, l.bar)
	start := store.DayBoundary(d.Day, l.dayStart)
	total := store.DayBoundary(d.Day.AddDate(0, 0, 1), l.dayStart).Sub(start)
	at := func(i float64) time.Time { return start.Add(time.Duration(i / float64(l.bar) * float64(total))) }
	pos := func(t time.Time) float64 { return min(max(float64(t.Sub(start))/float64(total), 0), 1) }
	index := func(t time.Time) int { return min(int(pos(t)*float64(l.bar)), l.bar-1) }

	weekend := store.IsWeekend(d.Day)
	var coreStart, coreEnd time.Time
	if !l.work.IsZero() && !weekend {
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

// renderBar draws the cells, one styled run per stretch of equal kind.
func (l layout) renderBar(cells []cellKind) string {
	var b strings.Builder
	for i := 0; i < len(cells); {
		j := i
		for j < len(cells) && cells[j] == cells[i] {
			j++
		}
		b.WriteString(l.styles.cell(cells[i]).Render(strings.Repeat(string(glyphs[cells[i]]), j-i)))
		i = j
	}
	return b.String()
}
