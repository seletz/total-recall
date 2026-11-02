package activity

import (
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
)

// TestCells classifies the cells of a 24-cell axis (one cell per hour from
// 04:00) for the edge cases of the issue: work inside, before, after the
// core hours and after midnight, a weekend, a day without activity and no
// working time at all.
func TestCells(t *testing.T) {
	loc := testfixture.ActivityLocation
	mon := time.Date(2026, 1, 5, 0, 0, 0, 0, loc) // Monday
	sat := time.Date(2026, 1, 10, 0, 0, 0, 0, loc)
	day := func(d time.Time, first, last time.Duration) store.Workday {
		return store.Workday{Day: d, First: d.Add(first), Last: d.Add(last)}
	}
	h, m := time.Hour, time.Minute
	tests := []struct {
		name string
		work store.WorkHours
		day  store.Workday
		want string
	}{
		{"inactive weekday", work, store.Workday{Day: mon}, "·····│───────│··········"},
		{"inactive weekend", work, store.Workday{Day: sat}, "························"},
		{"inside core hours", work, day(mon, 9*h+15*m, 9*h+40*m), "·····█───────│··········"},
		{"whole core hours", work, day(mon, 9*h, 17*h), "·····████████│··········"},
		{"early single event", work, day(mon, 8*h, 8*h), "····▓│───────│··········"},
		{"late single event", work, day(mon, 20*h+30*m, 20*h+30*m), "·····│───────│··▓·······"},
		{"entirely after core hours", work, day(mon, 17*h+30*m, 17*h+55*m), "·····│───────▓··········"},
		{"across midnight", work, day(mon, 23*h+40*m, 24*h+5*m), "·····│───────│·····▓▒···"},
		{"early to late", work, day(mon, 7*h, 19*h), "···▓▓████████▓▓·········"},
		{"weekend", work, day(sat, 11*h, 11*h), "·······█················"},
		{"weekend after midnight", work, day(sat, 23*h, 25*h), "···················██···"},
		{"no working time", store.WorkHours{}, day(mon, 9*h+15*m, 9*h+40*m), "·····█··················"},
		{"no working time inactive", store.WorkHours{}, store.Workday{Day: mon}, "························"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := layout{loc: loc, dayStart: 4 * time.Hour, work: tt.work, bar: 24}
			cells := l.cells(tt.day)
			got := make([]rune, len(cells))
			for i, c := range cells {
				got[i] = glyphs[c]
			}
			if string(got) != tt.want {
				t.Errorf("cells =\n%s\nwant\n%s", string(got), tt.want)
			}
		})
	}
}

// TestCellsMidnightDayStart checks the axis from midnight: there is no
// night segment, the core hours sit where the clock says.
func TestCellsMidnightDayStart(t *testing.T) {
	loc := testfixture.ActivityLocation
	mon := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	l := layout{loc: loc, dayStart: 0, work: work, bar: 24}
	cells := l.cells(store.Workday{Day: mon, First: mon.Add(23*time.Hour + 40*time.Minute), Last: mon.Add(23*time.Hour + 40*time.Minute)})
	got := make([]rune, len(cells))
	for i, c := range cells {
		got[i] = glyphs[c]
	}
	if want := "·········│───────│·····▓"; string(got) != want {
		t.Errorf("cells =\n%s\nwant\n%s", string(got), want)
	}
}
