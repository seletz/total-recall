package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProjectDays is the counted activity of a range of days by project: for
// every day, which project had events in which minute, so a view can show
// which project was worked on when. Projects are keyed like the dimension
// DimProject of ActivitySeries (the session's git root, else its working
// directory; subagent sessions take their parent's), so the same names and
// colours apply.
type ProjectDays struct {
	// Keys are the project keys of the range by counted events over the
	// whole range, descending, then by key.
	Keys []string
	// Events are the counted events per entry of Keys, in the same order.
	Events []int
	// Dirs are the Keys that are working directories of sessions without
	// a repository, sorted (see ActivitySeries.Dirs).
	Dirs []string
	// Days has one entry per day from the first to the last day of the
	// range, gap-free like Workday.
	Days []ProjectDay
}

// ProjectDay is the project activity of one day under the day rule.
type ProjectDay struct {
	Day   time.Time // local midnight of the calendar day the activity is attributed to
	Label string    // Day as 2006-01-02
	// Slots are the counted events by minute and project, ascending by
	// Offset, then by Project; empty on a day without activity.
	Slots []ProjectSlot
}

// ProjectSlot is the counted events of one project in one minute of a day.
type ProjectSlot struct {
	Project int           // index into ProjectDays.Keys
	Offset  time.Duration // start of the minute, since the day boundary (DayBoundary)
	Events  int
}

// ProjectDays returns, per day between f.Since and f.Until, the counted
// events by project and minute. Days are attributed like Workday: in
// f.Location with the day beginning at f.DayStart, human events only with
// f.HumanOnly, else human and agent events; the range of days is
// DayOf(Since) to DayOf(Until) minus one, an open bound takes the first or
// last day with activity. f.Holidays is not used.
func (s *Store) ProjectDays(ctx context.Context, f WorkdayFilter) (ProjectDays, error) {
	if f.DayStart < 0 || f.DayStart >= 24*time.Hour {
		return ProjectDays{}, fmt.Errorf("store: project days: day start %s out of range", f.DayStart)
	}
	loc := f.Location
	if loc == nil {
		loc = time.UTC
	}
	cond := []string{"e.actor IN ('human', 'agent')"}
	if f.HumanOnly {
		cond[0] = "e.actor = 'human'"
	}
	var args []any
	if !f.Since.IsZero() {
		cond = append(cond, "e.ts >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		cond = append(cond, "e.ts < ?")
		args = append(args, formatTime(f.Until))
	}
	rows, err := s.db().QueryContext(ctx, `SELECT e.ts,
			COALESCE(s.cwd, ''), COALESCE(s.git_root, ''), COALESCE(p.cwd, ''), COALESCE(p.git_root, '')
		FROM events e
		JOIN sessions s ON s.id = e.session_id
		LEFT JOIN sessions p ON p.id = s.parent_id
		WHERE `+strings.Join(cond, " AND "), args...)
	if err != nil {
		return ProjectDays{}, fmt.Errorf("store: project days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type minute struct {
		key    string
		offset time.Duration
	}
	days := map[time.Time]map[minute]int{} // day -> minute -> events
	totals := map[string]int{}
	isDir := map[string]bool{}
	var firstDay, lastDay time.Time
	for rows.Next() {
		var tsv sql.NullString
		var cwd, gitRoot, pcwd, pgitRoot string
		if err := rows.Scan(&tsv, &cwd, &gitRoot, &pcwd, &pgitRoot); err != nil {
			return ProjectDays{}, fmt.Errorf("store: project days: %w", err)
		}
		t := parseTime(tsv)
		if t.IsZero() {
			continue
		}
		key, dir := activityKey(DimProject, "", "", cwd, gitRoot, pcwd, pgitRoot, "", "")
		if seen, ok := isDir[key]; !ok || seen {
			isDir[key] = dir
		}
		day := DayOf(t, loc, f.DayStart)
		m := days[day]
		if m == nil {
			m = map[minute]int{}
			days[day] = m
		}
		m[minute{key, t.Sub(DayBoundary(day, f.DayStart)).Truncate(time.Minute)}]++
		totals[key]++
		if firstDay.IsZero() || day.Before(firstDay) {
			firstDay = day
		}
		if day.After(lastDay) {
			lastDay = day
		}
	}
	if err := rows.Err(); err != nil {
		return ProjectDays{}, fmt.Errorf("store: project days: %w", err)
	}

	var out ProjectDays
	for k := range totals {
		out.Keys = append(out.Keys, k)
	}
	sort.Slice(out.Keys, func(i, j int) bool {
		if totals[out.Keys[i]] != totals[out.Keys[j]] {
			return totals[out.Keys[i]] > totals[out.Keys[j]]
		}
		return out.Keys[i] < out.Keys[j]
	})
	index := make(map[string]int, len(out.Keys))
	for i, k := range out.Keys {
		index[k] = i
		out.Events = append(out.Events, totals[k])
		if isDir[k] {
			out.Dirs = append(out.Dirs, k)
		}
	}
	sort.Strings(out.Dirs)

	from, to := firstDay, lastDay
	if !f.Since.IsZero() {
		from = DayOf(f.Since, loc, f.DayStart)
	}
	if !f.Until.IsZero() {
		to = DayOf(f.Until.Add(-time.Nanosecond), loc, f.DayStart)
	}
	if from.IsZero() || to.IsZero() {
		return out, nil
	}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		d := ProjectDay{Day: day, Label: day.Format("2006-01-02")}
		for m, n := range days[day] {
			d.Slots = append(d.Slots, ProjectSlot{Project: index[m.key], Offset: m.offset, Events: n})
		}
		sort.Slice(d.Slots, func(i, j int) bool {
			if d.Slots[i].Offset != d.Slots[j].Offset {
				return d.Slots[i].Offset < d.Slots[j].Offset
			}
			return d.Slots[i].Project < d.Slots[j].Project
		})
		out.Days = append(out.Days, d)
	}
	return out, nil
}
