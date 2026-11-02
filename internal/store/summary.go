package store

import (
	"fmt"
	"sort"
	"time"
)

// Figure is the minimum, median, mean and maximum of one quantity over the
// active days of a range.
type Figure struct {
	Min, Median, Mean, Max time.Duration
}

// Summary holds the figures of the active days of a range: the first and
// the last activity as wall-clock offsets from the day boundary (see
// ClockOffset) and the span. Active is the number of days summarised; with
// none every figure is zero.
type Summary struct {
	First, Last, Span Figure
	Active            int
}

// Summarize computes the figures of the active days; dayStart is the day
// boundary the clock offsets refer to.
func Summarize(days []Workday, dayStart time.Duration) Summary {
	var first, last, span []time.Duration
	for _, d := range days {
		if !d.Active() {
			continue
		}
		first = append(first, ClockOffset(d.First, d.Day, dayStart))
		last = append(last, ClockOffset(d.Last, d.Day, dayStart))
		span = append(span, d.Span())
	}
	return Summary{First: figure(first), Last: figure(last), Span: figure(span), Active: len(span)}
}

// figure computes the figure of the values; zero without values.
func figure(v []time.Duration) Figure {
	if len(v) == 0 {
		return Figure{}
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	var sum time.Duration
	for _, d := range v {
		sum += d
	}
	f := Figure{Min: v[0], Max: v[len(v)-1], Mean: sum / time.Duration(len(v))}
	if n := len(v); n%2 == 1 {
		f.Median = v[n/2]
	} else {
		f.Median = (v[n/2-1] + v[n/2]) / 2
	}
	return f
}

// ClockOffset is the wall-clock time of t (in day's location) as an
// offset from the day boundary of day: 09:15 with a boundary of 04:00 is
// 5h15m, 01:30 the next day is 21h30m. Unlike the elapsed time it is not
// affected by a daylight saving change, so it matches the clock strings
// of Workday.
func ClockOffset(t, day time.Time, dayStart time.Duration) time.Duration {
	l := t.In(day.Location())
	h, m, s := l.Clock()
	y, mo, d := l.Date()
	days := daysBetween(day, time.Date(y, mo, d, 0, 0, 0, 0, day.Location()))
	return time.Duration(days)*24*time.Hour + time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second - dayStart
}

// FormatClock formats a ClockOffset as a clock time like Workday's
// FirstClock: "09:15", past midnight "01:30 (+1)".
func FormatClock(offset, dayStart time.Duration) string {
	wall := offset + dayStart
	days := int(wall / (24 * time.Hour))
	wall -= time.Duration(days) * 24 * time.Hour
	s := fmt.Sprintf("%02d:%02d", int(wall.Hours()), int(wall.Minutes())%60)
	if days > 0 {
		s += fmt.Sprintf(" (+%d)", days)
	}
	return s
}
