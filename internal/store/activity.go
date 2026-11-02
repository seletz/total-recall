package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Period is the bucket width of an ActivitySeries.
type Period int

// Periods of ActivitySeries.
const (
	PeriodDay   Period = iota // local calendar day, label 2006-01-02
	PeriodWeek                // ISO week starting Monday, label 2006-W02
	PeriodMonth               // calendar month, label 2006-01
)

// String returns day, week or month.
func (p Period) String() string {
	switch p {
	case PeriodDay:
		return "day"
	case PeriodWeek:
		return "week"
	case PeriodMonth:
		return "month"
	}
	return fmt.Sprintf("Period(%d)", int(p))
}

// ParsePeriod returns the Period named by s: day, week or month.
func ParsePeriod(s string) (Period, error) {
	for _, p := range []Period{PeriodDay, PeriodWeek, PeriodMonth} {
		if p.String() == s {
			return p, nil
		}
	}
	return 0, fmt.Errorf("invalid period %q (day, week, month)", s)
}

// Dimension is the grouping of an ActivitySeries within each period.
type Dimension int

// Dimensions of ActivitySeries.
const (
	// DimProject groups by the raw project key: the session's git root, else
	// its cwd. Subagent sessions take the key of their parent session. The
	// mapping to a display name is left to the caller.
	DimProject Dimension = iota
	// DimAgent groups by "<harness> main" (no parent) or "<harness> subagent".
	DimAgent
	// DimDirectory groups by cwd. Subagent sessions take the cwd of their
	// parent session.
	DimDirectory
)

// String returns project, agent or directory.
func (d Dimension) String() string {
	switch d {
	case DimProject:
		return "project"
	case DimAgent:
		return "agent"
	case DimDirectory:
		return "directory"
	}
	return fmt.Sprintf("Dimension(%d)", int(d))
}

// ParseDimension returns the Dimension named by s: project, agent or
// directory.
func ParseDimension(s string) (Dimension, error) {
	for _, d := range []Dimension{DimProject, DimAgent, DimDirectory} {
		if d.String() == s {
			return d, nil
		}
	}
	return 0, fmt.Errorf("invalid dimension %q (project, agent, directory)", s)
}

// OtherKey is the key under which ActivitySeries folds the dimension values
// beyond TopN.
const OtherKey = "other"

// ActivityFilter selects the range, resolution and grouping of ActivitySeries.
type ActivityFilter struct {
	// Since/Until select events by time in [Since, Until). A zero Since starts
	// at the first matching event, a zero Until ends after the last one.
	Since, Until time.Time
	// Location is the time zone for period boundaries; default UTC.
	Location  *time.Location
	Period    Period
	Dimension Dimension
	// TopN keeps the TopN largest keys (by total human+agent events over the
	// whole range) and folds the rest of every period into OtherKey, which is
	// always the last key. 0 keeps all keys.
	TopN int
}

// ActivityCell is the activity of one dimension value in one period.
type ActivityCell struct {
	Key string // project key, "<harness> main"/"<harness> subagent", directory or OtherKey
	Activity
}

// ActivityBucket is one period of an ActivitySeries.
type ActivityBucket struct {
	Start time.Time // start of the period in the filter's Location
	Label string    // 2006-01-02, 2006-W02 or 2006-01
	// Cells has one entry per ActivitySeries.Keys in the same order, so
	// Cells[i].Key == Keys[i]; cells without activity are zero.
	Cells []ActivityCell
}

// ActivitySeries is activity per period and dimension value.
type ActivitySeries struct {
	Buckets []ActivityBucket // every period between Since and Until, including empty ones
	Keys    []string         // dimension values by total volume descending, then by key; OtherKey last
}

// periodStart returns the start of the period containing t, in t's location.
func periodStart(t time.Time, p Period) time.Time {
	y, m, d := t.Date()
	switch p {
	case PeriodWeek:
		d -= (int(t.Weekday()) + 6) % 7 // back to Monday
	case PeriodMonth:
		d = 1
	}
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// periodNext returns the start of the period after the one starting at start.
func periodNext(start time.Time, p Period) time.Time {
	y, m, d := start.Date()
	switch p {
	case PeriodWeek:
		d += 7
	case PeriodMonth:
		m++
	default:
		d++
	}
	return time.Date(y, m, d, 0, 0, 0, 0, start.Location())
}

// periodLabel formats the start of a period.
func periodLabel(start time.Time, p Period) string {
	switch p {
	case PeriodWeek:
		y, w := start.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	case PeriodMonth:
		return start.Format("2006-01")
	}
	return start.Format("2006-01-02")
}

// activityKey derives the dimension value of a session. Parent columns are
// empty when the session has no parent or the parent is unknown.
func activityKey(d Dimension, harness, parentID, cwd, gitRoot, parentCwd, parentGitRoot string) string {
	switch d {
	case DimAgent:
		if parentID != "" {
			return harness + " subagent"
		}
		return harness + " main"
	case DimDirectory:
		if parentCwd != "" {
			return parentCwd
		}
		return cwd
	}
	if parentGitRoot != "" || parentCwd != "" {
		gitRoot, cwd = parentGitRoot, parentCwd
	}
	if gitRoot != "" {
		return gitRoot
	}
	return cwd
}

func (b *bucket) merge(o *bucket) {
	for id := range o.sessions {
		if b.sessions == nil {
			b.sessions = map[string]bool{}
		}
		b.sessions[id] = true
	}
	b.human += o.human
	b.agent += o.agent
}

// ActivitySeries aggregates human and agent events per period and
// dimension value. Like Stats it runs a single query over events ⨝ sessions
// and buckets in Go in f.Location. Subagent sessions resolve one level up to
// their parent for DimProject and DimDirectory.
func (s *Store) ActivitySeries(ctx context.Context, f ActivityFilter) (ActivitySeries, error) {
	switch f.Period {
	case PeriodDay, PeriodWeek, PeriodMonth:
	default:
		return ActivitySeries{}, fmt.Errorf("store: activity: invalid period %s", f.Period)
	}
	switch f.Dimension {
	case DimProject, DimAgent, DimDirectory:
	default:
		return ActivitySeries{}, fmt.Errorf("store: activity: invalid dimension %s", f.Dimension)
	}
	loc := f.Location
	if loc == nil {
		loc = time.UTC
	}
	cond := []string{"e.actor IN ('human', 'agent')"}
	var args []any
	if !f.Since.IsZero() {
		cond = append(cond, "e.ts >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		cond = append(cond, "e.ts < ?")
		args = append(args, formatTime(f.Until))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.ts, e.actor, e.session_id,
			s.harness, COALESCE(s.parent_id, ''), COALESCE(s.cwd, ''), COALESCE(s.git_root, ''),
			COALESCE(p.cwd, ''), COALESCE(p.git_root, '')
		FROM events e
		JOIN sessions s ON s.id = e.session_id
		LEFT JOIN sessions p ON p.id = s.parent_id
		WHERE `+strings.Join(cond, " AND "), args...)
	if err != nil {
		return ActivitySeries{}, fmt.Errorf("store: activity: %w", err)
	}
	defer func() { _ = rows.Close() }()
	periods := map[time.Time]map[string]*bucket{} // period start -> key -> bucket
	totals := map[string]int{}
	var first, last time.Time
	for rows.Next() {
		var tsv sql.NullString
		var actor, session, harness, parentID, cwd, gitRoot, pcwd, pgitRoot string
		if err := rows.Scan(&tsv, &actor, &session, &harness, &parentID, &cwd, &gitRoot, &pcwd, &pgitRoot); err != nil {
			return ActivitySeries{}, fmt.Errorf("store: activity: %w", err)
		}
		t := parseTime(tsv)
		if t.IsZero() {
			continue
		}
		start := periodStart(t.In(loc), f.Period)
		key := activityKey(f.Dimension, harness, parentID, cwd, gitRoot, pcwd, pgitRoot)
		keys := periods[start]
		if keys == nil {
			keys = map[string]*bucket{}
			periods[start] = keys
		}
		if keys[key] == nil {
			keys[key] = &bucket{}
		}
		keys[key].add(session, actor)
		totals[key]++
		if first.IsZero() || start.Before(first) {
			first = start
		}
		if start.After(last) {
			last = start
		}
	}
	if err := rows.Err(); err != nil {
		return ActivitySeries{}, fmt.Errorf("store: activity: %w", err)
	}

	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if totals[keys[i]] != totals[keys[j]] {
			return totals[keys[i]] > totals[keys[j]]
		}
		return keys[i] < keys[j]
	})
	fold := f.TopN > 0 && len(keys) > f.TopN
	if fold {
		keys = append(keys[:f.TopN:f.TopN], OtherKey)
	}
	top := map[string]bool{}
	for _, k := range keys {
		top[k] = true
	}

	var since, until time.Time
	if f.Since.IsZero() {
		since = first
	} else {
		since = periodStart(f.Since.In(loc), f.Period)
	}
	if f.Until.IsZero() {
		if !last.IsZero() {
			until = periodNext(last, f.Period)
		}
	} else {
		until = f.Until.In(loc)
	}
	out := ActivitySeries{Keys: keys}
	if since.IsZero() || until.IsZero() {
		return out, nil
	}
	for start := since; start.Before(until); start = periodNext(start, f.Period) {
		b := ActivityBucket{Start: start, Label: periodLabel(start, f.Period), Cells: make([]ActivityCell, len(keys))}
		for i, k := range keys {
			b.Cells[i].Key = k
		}
		var other bucket
		for k, kb := range periods[start] {
			if fold && !top[k] {
				other.merge(kb)
				continue
			}
			for i, kk := range keys {
				if kk == k {
					b.Cells[i].Activity = kb.activity()
					break
				}
			}
		}
		if fold {
			b.Cells[len(keys)-1].Activity = other.activity()
		}
		out.Buckets = append(out.Buckets, b)
	}
	return out, nil
}
