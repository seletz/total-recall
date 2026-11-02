package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// WorkdayFilter selects the range and the day rule of Workday.
type WorkdayFilter struct {
	// Since/Until select events by time in [Since, Until). A zero Since
	// starts at the first matching event, a zero Until ends after the last
	// one. Callers that want whole days pass DayBoundary instants.
	Since, Until time.Time
	// Location is the time zone of the days; default UTC.
	Location *time.Location
	// DayStart is the wall-clock time at which a day begins, as an offset
	// from midnight (4h = 04:00): events before it belong to the previous
	// day. 0 means midnight. Must be below 24h.
	DayStart time.Duration
	// HumanOnly counts human events only; otherwise human and agent events.
	HumanOnly bool
}

// Workday is the first and the last activity of one day under the
// WorkdayFilter's day rule.
type Workday struct {
	Day   time.Time // local midnight of the calendar day the activity is attributed to
	Label string    // Day as 2006-01-02
	// First and Last are the instants of the first and the last counted
	// event; zero on a day without activity.
	First, Last time.Time
	Activity
}

// Active reports whether the day has any counted activity.
func (d Workday) Active() bool { return !d.First.IsZero() }

// Span is the elapsed time between the first and the last activity.
func (d Workday) Span() time.Duration {
	if !d.Active() {
		return 0
	}
	return d.Last.Sub(d.First)
}

// FirstClock formats First as a local clock time ("08:12"), with the number
// of days after Day in brackets when it is past midnight ("01:30 (+1)").
// Empty for a day without activity.
func (d Workday) FirstClock(loc *time.Location) string { return d.clock(d.First, loc) }

// LastClock formats Last like FirstClock.
func (d Workday) LastClock(loc *time.Location) string { return d.clock(d.Last, loc) }

func (d Workday) clock(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	l := t.In(loc)
	s := l.Format("15:04")
	y, m, dd := l.Date()
	if n := daysBetween(d.Day, time.Date(y, m, dd, 0, 0, 0, 0, loc)); n > 0 {
		s += fmt.Sprintf(" (+%d)", n)
	}
	return s
}

// daysBetween counts the calendar days from a to b (local midnights).
func daysBetween(a, b time.Time) int {
	// Round, not truncate: a DST change makes a local day 23 or 25 hours.
	return int((b.Sub(a) + 12*time.Hour) / (24 * time.Hour))
}

// WorkdayTotal sums the spans of the days and counts those with activity.
func WorkdayTotal(days []Workday) (span time.Duration, active int) {
	for _, d := range days {
		if d.Active() {
			span += d.Span()
			active++
		}
	}
	return span, active
}

// DayOf returns the local midnight of the day t is attributed to under the
// rule that a day starts at dayStart (wall-clock offset from midnight).
func DayOf(t time.Time, loc *time.Location, dayStart time.Duration) time.Time {
	l := t.In(loc)
	y, m, d := l.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if l.Before(DayBoundary(day, dayStart)) {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

// DayBoundary returns the instant at which the given day (any time of it,
// in its location) begins: its date at the wall-clock time dayStart. The
// boundary keeps its clock time across daylight saving changes.
func DayBoundary(day time.Time, dayStart time.Duration) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, 0, int(dayStart/time.Minute), int(dayStart%time.Minute/time.Second), 0, day.Location())
}

// Workday returns, per day between Since and Until, the first and the last
// counted event with the usual counts. Days are attributed in f.Location
// with the day beginning at f.DayStart; days without activity are included
// as inactive entries, so the result is gap-free from the first to the last
// day of the range. The range of days is DayOf(Since) to DayOf(Until) minus
// one; an open bound takes the first or last day with activity.
func (s *Store) Workday(ctx context.Context, f WorkdayFilter) ([]Workday, error) {
	if f.DayStart < 0 || f.DayStart >= 24*time.Hour {
		return nil, fmt.Errorf("store: workday: day start %s out of range", f.DayStart)
	}
	loc := f.Location
	if loc == nil {
		loc = time.UTC
	}
	cond := []string{"actor IN ('human', 'agent')"}
	if f.HumanOnly {
		cond[0] = "actor = 'human'"
	}
	var args []any
	if !f.Since.IsZero() {
		cond = append(cond, "ts >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		cond = append(cond, "ts < ?")
		args = append(args, formatTime(f.Until))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ts, actor, session_id FROM events WHERE `+strings.Join(cond, " AND "), args...)
	if err != nil {
		return nil, fmt.Errorf("store: workday: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type dayAcc struct {
		bucket
		first, last time.Time
	}
	days := map[time.Time]*dayAcc{}
	var firstDay, lastDay time.Time
	for rows.Next() {
		var tsv sql.NullString
		var actor, session string
		if err := rows.Scan(&tsv, &actor, &session); err != nil {
			return nil, fmt.Errorf("store: workday: %w", err)
		}
		t := parseTime(tsv)
		if t.IsZero() {
			continue
		}
		day := DayOf(t, loc, f.DayStart)
		a := days[day]
		if a == nil {
			a = &dayAcc{first: t, last: t}
			days[day] = a
		}
		a.add(session, actor)
		if t.Before(a.first) {
			a.first = t
		}
		if t.After(a.last) {
			a.last = t
		}
		if firstDay.IsZero() || day.Before(firstDay) {
			firstDay = day
		}
		if day.After(lastDay) {
			lastDay = day
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: workday: %w", err)
	}

	from, to := firstDay, lastDay
	if !f.Since.IsZero() {
		from = DayOf(f.Since, loc, f.DayStart)
	}
	if !f.Until.IsZero() {
		to = DayOf(f.Until.Add(-time.Nanosecond), loc, f.DayStart)
	}
	if from.IsZero() || to.IsZero() {
		return nil, nil
	}
	var out []Workday
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		w := Workday{Day: day, Label: day.Format("2006-01-02")}
		if a := days[day]; a != nil {
			w.First, w.Last, w.Activity = a.first, a.last, a.activity()
		}
		out = append(out, w)
	}
	return out, nil
}
