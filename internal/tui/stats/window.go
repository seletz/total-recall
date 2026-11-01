package stats

import (
	"time"

	"github.com/seletz/total-recall/internal/store"
)

// Default window sizes per period, in periods.
const (
	DefaultDays   = 28
	DefaultWeeks  = 16
	DefaultMonths = 12
)

// MaxWindows is how many default windows a zoomed-out window may span.
const MaxWindows = 8

// DefaultCount returns the number of periods the default window of p shows.
func DefaultCount(p store.Period) int {
	switch p {
	case store.PeriodWeek:
		return DefaultWeeks
	case store.PeriodMonth:
		return DefaultMonths
	}
	return DefaultDays
}

// Window is the range of whole periods the view shows: Count periods of
// Period, the last of which contains Anchor (an instant in the view's
// location, initially now). Since and Until are the bounds for
// store.ActivityFilter; the store buckets the same way, so the series has
// exactly Count buckets. The anchor rather than a period start is kept so
// that switching the period back and forth returns to the same window.
type Window struct {
	Period store.Period
	Anchor time.Time
	Count  int
}

// NewWindow returns the default window of p that ends with the period
// containing now, in loc.
func NewWindow(p store.Period, now time.Time, loc *time.Location) Window {
	return Window{Period: p, Anchor: now.In(loc), Count: DefaultCount(p)}
}

// Last is the start of the last period.
func (w Window) Last() time.Time { return periodStart(w.Anchor, w.Period) }

// Since is the start of the first period.
func (w Window) Since() time.Time { return addPeriods(w.Last(), w.Period, 1-w.Count) }

// Until is the start of the period after the last one (exclusive bound).
func (w Window) Until() time.Time { return addPeriods(w.Last(), w.Period, 1) }

// Shift moves the window by n windows: forward for positive n, back for
// negative. The anchor keeps its time of day and, for months, its day of
// the month as far as the target month has it.
func (w Window) Shift(n int) Window {
	y, m, d := w.Anchor.Date()
	hh, mm, ss := w.Anchor.Clock()
	loc := w.Anchor.Location()
	switch w.Period {
	case store.PeriodWeek:
		d += 7 * n * w.Count
	case store.PeriodMonth:
		m += time.Month(n * w.Count)
		first := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		d = min(d, daysIn(first))
		y, m = first.Year(), first.Month()
	default:
		d += n * w.Count
	}
	w.Anchor = time.Date(y, m, d, hh, mm, ss, w.Anchor.Nanosecond(), loc)
	return w
}

// Zoom doubles the count n times (halves it for negative n), keeping the
// end of the window. The count stays between one period and MaxWindows
// default windows.
func (w Window) Zoom(n int) Window {
	for ; n > 0; n-- {
		w.Count *= 2
	}
	for ; n < 0; n++ {
		w.Count /= 2
	}
	w.Count = min(max(w.Count, 1), MaxWindows*DefaultCount(w.Period))
	return w
}

// WithPeriod switches to the default window of p that ends with the period
// containing the anchor.
func (w Window) WithPeriod(p store.Period) Window {
	return Window{Period: p, Anchor: w.Anchor, Count: DefaultCount(p)}
}

// Equal reports whether both windows show the same periods.
func (w Window) Equal(o Window) bool {
	return w.Period == o.Period && w.Count == o.Count && w.Last().Equal(o.Last())
}

// String formats the window as the first and the last day shown, like
// "2025-12-15 – 2026-01-11".
func (w Window) String() string {
	return w.Since().Format("2006-01-02") + " – " + w.Until().AddDate(0, 0, -1).Format("2006-01-02")
}

// daysIn returns the number of days of the month that starts at first.
func daysIn(first time.Time) int {
	return first.AddDate(0, 1, -1).Day()
}

// periodStart returns the start of the period containing t, in t's location
// (the same rule as the store).
func periodStart(t time.Time, p store.Period) time.Time {
	y, m, d := t.Date()
	switch p {
	case store.PeriodWeek:
		d -= (int(t.Weekday()) + 6) % 7 // back to Monday
	case store.PeriodMonth:
		d = 1
	}
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// addPeriods returns the period start n periods after start (before it for
// negative n). time.Date normalises the day and month, and keeps local
// midnight across daylight saving changes.
func addPeriods(start time.Time, p store.Period, n int) time.Time {
	y, m, d := start.Date()
	switch p {
	case store.PeriodWeek:
		d += 7 * n
	case store.PeriodMonth:
		m += time.Month(n)
	default:
		d += n
	}
	return time.Date(y, m, d, 0, 0, 0, 0, start.Location())
}
