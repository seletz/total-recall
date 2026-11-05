package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProjectDay is the counted activity of one day by project under the day
// rule: which project had events in which minute, so a view can show which
// project was worked on when. Projects are keyed like the dimension
// DimProject of ActivitySeries (the session's git root, else its working
// directory; subagent sessions take their parent's), so the same names and
// colours apply. Every day stands on its own, so days can be cached one by
// one.
type ProjectDay struct {
	Day   time.Time // local midnight of the calendar day the activity is attributed to
	Label string    // Day as 2006-01-02
	// Projects are the projects with counted events on the day, by events
	// descending, then by key; empty on a day without activity.
	Projects []DayProject
}

// DayProject is the counted activity of one project on one day.
type DayProject struct {
	Key string // project key, see ProjectDay
	// Dir reports that the key is the working directory of sessions
	// without a repository, like ActivitySeries.Dirs lists them; display
	// code shows such keys as paths.
	Dir    bool
	Events int           // counted events of the day
	Slots  []ProjectSlot // the events by minute, ascending
}

// ProjectSlot is the counted events of one project in one minute of a day.
type ProjectSlot struct {
	Offset time.Duration // start of the minute, since the day boundary (DayBoundary)
	Events int
}

// ProjectDays returns, per day between f.Since and f.Until, the counted
// events by project and minute. Days are attributed like Workday: in
// f.Location with the day beginning at f.DayStart, human events only with
// f.HumanOnly, else human and agent events; the range of days is
// DayOf(Since) to DayOf(Until) minus one, gap-free, an open bound takes
// the first or last day with activity. f.Holidays is not used.
func (s *Store) ProjectDays(ctx context.Context, f WorkdayFilter) ([]ProjectDay, error) {
	if f.DayStart < 0 || f.DayStart >= 24*time.Hour {
		return nil, fmt.Errorf("store: project days: day start %s out of range", f.DayStart)
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
		return nil, fmt.Errorf("store: project days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type minute struct {
		key    string
		offset time.Duration
	}
	days := map[time.Time]map[minute]int{} // day -> minute -> events
	isDir := map[string]bool{}
	var firstDay, lastDay time.Time
	for rows.Next() {
		var tsv sql.NullString
		var cwd, gitRoot, pcwd, pgitRoot string
		if err := rows.Scan(&tsv, &cwd, &gitRoot, &pcwd, &pgitRoot); err != nil {
			return nil, fmt.Errorf("store: project days: %w", err)
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
		if firstDay.IsZero() || day.Before(firstDay) {
			firstDay = day
		}
		if day.After(lastDay) {
			lastDay = day
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: project days: %w", err)
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
	var out []ProjectDay
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		d := ProjectDay{Day: day, Label: day.Format("2006-01-02")}
		byKey := map[string]*DayProject{}
		for m, n := range days[day] {
			p := byKey[m.key]
			if p == nil {
				p = &DayProject{Key: m.key, Dir: isDir[m.key]}
				byKey[m.key] = p
			}
			p.Events += n
			p.Slots = append(p.Slots, ProjectSlot{Offset: m.offset, Events: n})
		}
		for _, p := range byKey {
			sort.Slice(p.Slots, func(i, j int) bool { return p.Slots[i].Offset < p.Slots[j].Offset })
			d.Projects = append(d.Projects, *p)
		}
		sort.Slice(d.Projects, func(i, j int) bool {
			if d.Projects[i].Events != d.Projects[j].Events {
				return d.Projects[i].Events > d.Projects[j].Events
			}
			return d.Projects[i].Key < d.Projects[j].Key
		})
		out = append(out, d)
	}
	return out, nil
}
