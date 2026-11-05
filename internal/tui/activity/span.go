package activity

import (
	"time"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/window"
)

// span is the range of days the view shows: the one period (a day, an ISO
// week or a month) containing the anchor of its window, moved by shift
// days. ←/→ move the anchor by a period and keep the shift, ↑/↓ change the
// shift by one day, so a week or a month becomes a sliding window that
// keeps its length: a week always has seven days, a month the days of the
// month the anchor is in. d/w/m, c and l reset the shift to a whole
// calendar period. Two spans are the same when they show the same days of
// the same period, however they were reached.
type span struct {
	window window.Window // always a single period
	shift  int           // days the window is moved from the period
}

// single is the whole period of p containing now, in loc.
func single(p store.Period, now time.Time, loc *time.Location) span {
	return span{window: window.Single(p, now, loc)}
}

// period is the period the span is made of.
func (s span) period() store.Period { return s.window.Period }

// since is the local midnight of the first day, until that of the day
// after the last one.
func (s span) since() time.Time { return s.window.Since().AddDate(0, 0, s.shift) }
func (s span) until() time.Time { return s.window.Until().AddDate(0, 0, s.shift) }

// len is the number of days the span shows.
func (s span) len() int {
	n := 0
	for day := s.since(); day.Before(s.until()); day = day.AddDate(0, 0, 1) {
		n++
	}
	return n
}

// days moves the span by n days: later for positive n, earlier for negative.
func (s span) days(n int) span {
	s.shift += n
	return s
}

// periods moves the span by n whole periods and keeps the day shift.
func (s span) periods(n int) span {
	s.window = s.window.Shift(n)
	return s
}

// withPeriod switches to the whole period of p containing the day the
// span's anchor lands on after the shift: from a shifted week, d shows
// the day the anchor points at, m the month around it.
func (s span) withPeriod(p store.Period) span {
	return span{window: window.Single(p, s.window.Anchor.AddDate(0, 0, s.shift), s.window.Anchor.Location())}
}

// equal reports whether both spans show the same days of the same period.
func (s span) equal(o span) bool {
	return s.period() == o.period() && s.since().Equal(o.since()) && s.until().Equal(o.until())
}

// aligned reports whether the span is a whole calendar period: a week
// from Monday to Sunday, a month from its first to its last day. A single
// day always is.
func (s span) aligned() bool {
	switch s.period() {
	case store.PeriodWeek:
		return s.since().Weekday() == time.Monday
	case store.PeriodMonth:
		return s.since().Day() == 1 && s.until().Day() == 1
	}
	return true
}

// clampUntil moves the span back by whole days until it ends at limit at
// the latest; a span that ends earlier is returned as it is.
func (s span) clampUntil(limit time.Time) span {
	for s.until().After(limit) {
		s.shift--
	}
	return s
}
