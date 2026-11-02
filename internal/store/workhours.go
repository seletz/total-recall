package store

import (
	"fmt"
	"time"
)

// WorkHours is the normal working time the days of Workday are measured
// against: the core hours of a calendar day, the daily target of a workday
// (a weekday that is no public holiday) and the spans that count as on
// target per day and per week. The zero value means no working time is
// configured: no core hours, no target, every rating RatingNone.
type WorkHours struct {
	// Start and End are the core hours as offsets from local midnight
	// (09:00–17:00 = 9h, 17h).
	Start, End time.Duration
	// Target is the daily target on workdays; weekends and holidays have
	// none.
	Target time.Duration
	// Low and High bound the span of a day that counts as on target.
	Low, High time.Duration
	// WeeklyLow and WeeklyHigh bound the working time of a week with five
	// workdays that counts as on target; a range with another number of
	// workdays scales them (see RateBalance). Both zero means the daily
	// bounds times the workdays are used instead.
	WeeklyLow, WeeklyHigh time.Duration
}

// WorkdaysPerWeek is the number of workdays the weekly bounds refer to.
const WorkdaysPerWeek = 5

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
	Target   time.Duration // Target × Workdays
	Balance  time.Duration // Span minus Target
	Active   int           // days with activity
	Workdays int           // workdays of the range: weekdays that are no holiday, up to asOf
	Holidays int           // public holidays in the range, weekends included
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

// TargetFor returns the target of the day: Target on an active workday,
// zero on a weekend, a holiday or a day without activity.
func (w WorkHours) TargetFor(d Workday) time.Duration {
	if !d.Active() || d.IsOff() {
		return 0
	}
	return w.Target
}

// Diff returns the span of the day minus its target; ok is false on a day
// without activity or without working time, where there is nothing to
// compare. On a weekend or a holiday the whole span is the difference.
func (w WorkHours) Diff(d Workday) (diff time.Duration, ok bool) {
	if w.IsZero() || !d.Active() {
		return 0, false
	}
	return d.Span() - w.TargetFor(d), true
}

// Rate rates the span of an active workday against Low and High; a
// weekend, a holiday, an inactive day and no working time are RatingNone.
func (w WorkHours) Rate(d Workday) Rating {
	if w.IsZero() || !d.Active() || d.IsOff() {
		return RatingNone
	}
	return rate(d.Span(), w.Low, w.High)
}

// Balance sums the days: the spans of the active days against Target ×
// the workdays of the range. A workday is a weekday that is no public
// holiday, with or without activity; days after asOf (a local midnight,
// usually today) are not workdays yet, a zero asOf counts every day. So
// a day without activity on a past workday lowers the balance, a holiday
// or a weekend does not.
func (w WorkHours) Balance(days []Workday, asOf time.Time) Balance {
	var b Balance
	for _, d := range days {
		if d.IsHoliday() {
			b.Holidays++
		}
		if d.Active() {
			b.Span += d.Span()
			b.Active++
		}
		if w.IsZero() || d.IsOff() || (!asOf.IsZero() && d.Day.After(asOf)) {
			continue
		}
		b.Workdays++
	}
	b.Target = w.Target * time.Duration(b.Workdays)
	b.Balance = b.Span - b.Target
	return b
}

// RateBalance rates the working time of a range against the weekly
// bounds scaled by its workdays: on target between WeeklyLow ×
// Workdays/5 and WeeklyHigh × Workdays/5, so a full week is measured
// against the bounds as configured. Without weekly bounds the daily
// bounds times the workdays apply. Without a workday there is nothing to
// rate.
func (w WorkHours) RateBalance(b Balance) Rating {
	if w.IsZero() || b.Workdays == 0 {
		return RatingNone
	}
	n := time.Duration(b.Workdays)
	low, high := n*w.Low, n*w.High
	if w.WeeklyLow > 0 || w.WeeklyHigh > 0 {
		low, high = w.WeeklyLow*n/WorkdaysPerWeek, w.WeeklyHigh*n/WorkdaysPerWeek
	}
	return rate(b.Span, low, high)
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
