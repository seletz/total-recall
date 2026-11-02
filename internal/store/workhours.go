package store

import (
	"fmt"
	"time"
)

// WorkHours is the normal working time the days of Workday are measured
// against: the core hours of a calendar day, the daily target of a weekday
// and the span that counts as on target. The zero value means no working
// time is configured: no core hours, no target, every rating RatingNone.
type WorkHours struct {
	// Start and End are the core hours as offsets from local midnight
	// (09:00–17:00 = 9h, 17h).
	Start, End time.Duration
	// Target is the daily target on weekdays; weekends have none.
	Target time.Duration
	// Low and High bound the span of a day that counts as on target.
	Low, High time.Duration
}

// Rating says how a span or a balance relates to the target.
type Rating int

// Ratings of WorkHours.Rate and RateBalance.
const (
	RatingNone  Rating = iota // no target: inactive day, weekend, no working time
	RatingUnder               // below the lower bound
	RatingOn                  // within the bounds
	RatingOver                // above the upper bound
)

// String returns none, under, on or over.
func (r Rating) String() string {
	switch r {
	case RatingUnder:
		return "under"
	case RatingOn:
		return "on"
	case RatingOver:
		return "over"
	}
	return "none"
}

// Balance sums a range of days against the target.
type Balance struct {
	Span     time.Duration // sum of the spans of the active days
	Target   time.Duration // target of the active weekdays
	Balance  time.Duration // Span minus Target
	Active   int           // days with activity
	Workdays int           // active weekdays, the days with a target
}

// IsZero reports whether no working time is configured.
func (w WorkHours) IsZero() bool { return w == WorkHours{} }

// IsWeekend reports whether the day is a Saturday or a Sunday.
func IsWeekend(day time.Time) bool {
	wd := day.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

// Core returns the core hours of the calendar day (local midnight) as
// instants; they keep their wall-clock times across daylight saving
// changes.
func (w WorkHours) Core(day time.Time) (start, end time.Time) {
	return DayBoundary(day, w.Start), DayBoundary(day, w.End)
}

// TargetFor returns the target of the day: Target on an active weekday,
// zero on a weekend or a day without activity.
func (w WorkHours) TargetFor(d Workday) time.Duration {
	if !d.Active() || IsWeekend(d.Day) {
		return 0
	}
	return w.Target
}

// Diff returns the span of the day minus its target; ok is false on a day
// without activity or without working time, where there is nothing to
// compare. On a weekend the whole span is the difference.
func (w WorkHours) Diff(d Workday) (diff time.Duration, ok bool) {
	if w.IsZero() || !d.Active() {
		return 0, false
	}
	return d.Span() - w.TargetFor(d), true
}

// Rate rates the span of an active weekday against Low and High; a
// weekend, an inactive day and no working time are RatingNone.
func (w WorkHours) Rate(d Workday) Rating {
	if w.IsZero() || !d.Active() || IsWeekend(d.Day) {
		return RatingNone
	}
	return rate(d.Span(), w.Low, w.High)
}

// Balance sums the days: the spans of the active days against the target
// of the active weekdays.
func (w WorkHours) Balance(days []Workday) Balance {
	var b Balance
	for _, d := range days {
		if !d.Active() {
			continue
		}
		b.Span += d.Span()
		b.Active++
		if w.IsZero() || IsWeekend(d.Day) {
			continue
		}
		b.Target += w.Target
		b.Workdays++
	}
	b.Balance = b.Span - b.Target
	return b
}

// RateBalance rates a balance with the daily tolerance scaled by the
// workdays: on target between Workdays × (Low − Target) and Workdays ×
// (High − Target). Without a workday there is nothing to rate.
func (w WorkHours) RateBalance(b Balance) Rating {
	if w.IsZero() || b.Workdays == 0 {
		return RatingNone
	}
	n := time.Duration(b.Workdays)
	return rate(b.Balance, n*(w.Low-w.Target), n*(w.High-w.Target))
}

func rate(v, low, high time.Duration) Rating {
	switch {
	case v < low:
		return RatingUnder
	case v > high:
		return RatingOver
	}
	return RatingOn
}

// FormatSpan formats a span compactly: "5s", "25m", "3h05m", "72h00m".
func FormatSpan(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// FormatDiff formats a difference with its sign and whole minutes:
// "+1h12m", "-0h30m".
func FormatDiff(d time.Duration) string {
	sign := "+"
	if d < 0 {
		sign, d = "-", -d
	}
	return fmt.Sprintf("%s%dh%02dm", sign, int(d.Hours()), int(d.Minutes())%60)
}
