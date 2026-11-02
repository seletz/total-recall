package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

// Range names of stats --workday --range.
var rangeNames = []string{"today", "this-week", "last-week", "this-month", "last-month"}

// WorkdayJSON is the --json output of stats --workday: the first and the
// last activity per day of the range.
type WorkdayJSON struct {
	Since     string `json:"since"`
	Until     string `json:"until"`
	Timezone  string `json:"timezone"`
	DayStart  string `json:"day_start"`  // HH:MM, activity before it counts for the previous day
	HumanOnly bool   `json:"human_only"` // only human events were counted
	// Days are all days of the range, in order; a day without activity has
	// empty times and zero counts.
	Days  []WorkdayDayJSON `json:"days"`
	Total WorkdayTotalJSON `json:"total"`
}

// WorkdayDayJSON is one day.
type WorkdayDayJSON struct {
	Day     string `json:"day"`     // 2006-01-02 in local time
	Weekday string `json:"weekday"` // Mon … Sun
	First   string `json:"first"`   // first activity, UTC; "" without activity
	Last    string `json:"last"`    // last activity, UTC
	// FirstLocal and LastLocal are local clock times ("08:12"); past
	// midnight the days after Day follow in brackets ("01:30 (+1)").
	FirstLocal  string `json:"first_local"`
	LastLocal   string `json:"last_local"`
	SpanSeconds int    `json:"span_seconds"` // elapsed time between first and last
	ActivityJSON
}

// WorkdayTotalJSON sums the range.
type WorkdayTotalJSON struct {
	Days        int `json:"days"` // days with activity
	SpanSeconds int `json:"span_seconds"`
}

// rangeBounds resolves a --range name relative to now under the day rule:
// the bounds are day boundaries (dayStart o'clock), so "today" shortly
// after midnight is still the previous calendar day. Weeks start on Monday.
func rangeBounds(name string, now time.Time, loc *time.Location, dayStart time.Duration) (time.Time, time.Time, error) {
	day := store.DayOf(now, loc, dayStart)
	y, m, _ := day.Date()
	var from, to time.Time
	switch name {
	case "today":
		from, to = day, day.AddDate(0, 0, 1)
	case "this-week", "last-week":
		monday := day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
		if name == "last-week" {
			monday = monday.AddDate(0, 0, -7)
		}
		from, to = monday, monday.AddDate(0, 0, 7)
	case "this-month", "last-month":
		first := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		if name == "last-month" {
			first = first.AddDate(0, -1, 0)
		}
		from, to = first, first.AddDate(0, 1, 0)
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --range %q (%s)", name, strings.Join(rangeNames, ", "))
	}
	return store.DayBoundary(from, dayStart), store.DayBoundary(to, dayStart), nil
}

// workdayBounds resolves --month or --since/--until under the day rule: a
// month and date-only arguments mean whole days from the boundary (an
// --until date includes that day), arguments with a time are taken as they
// are. Empty arguments stay open.
func workdayBounds(month, sinceArg, untilArg string, loc *time.Location, dayStart time.Duration) (time.Time, time.Time, error) {
	since, until, err := statsPeriod(month, sinceArg, untilArg, time.Time{}, loc)
	if err != nil {
		return since, until, err
	}
	if month != "" || isDate(sinceArg) {
		since = store.DayBoundary(since, dayStart)
	}
	if month != "" || isDate(untilArg) {
		until = store.DayBoundary(until, dayStart)
	}
	return since, until, nil
}

// isDate reports whether s is a date-only time argument.
func isDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// workdayJSON converts the days to their JSON form.
func workdayJSON(days []store.Workday, since, until time.Time, loc *time.Location, tz, dayStart string, humanOnly bool) WorkdayJSON {
	out := WorkdayJSON{
		Since: isoTime(since), Until: isoTime(until), Timezone: tz, DayStart: dayStart, HumanOnly: humanOnly,
		Days: make([]WorkdayDayJSON, 0, len(days)),
	}
	for _, d := range days {
		out.Days = append(out.Days, WorkdayDayJSON{
			Day: d.Label, Weekday: d.Day.Format("Mon"),
			First: isoTime(d.First), Last: isoTime(d.Last),
			FirstLocal: d.FirstClock(loc), LastLocal: d.LastClock(loc),
			SpanSeconds: int(d.Span().Seconds()), ActivityJSON: act(d.Activity),
		})
	}
	span, active := store.WorkdayTotal(days)
	out.Total = WorkdayTotalJSON{Days: active, SpanSeconds: int(span.Seconds())}
	return out
}

// writeWorkdayTable writes the range, one row per day (DATE DAY FIRST LAST
// SPAN, "-" without activity) and the total line.
func writeWorkdayTable(w io.Writer, days []store.Workday, since, until time.Time, loc *time.Location, dayStart string, humanOnly bool) error {
	rng := "all time"
	if !since.IsZero() || !until.IsZero() {
		end := ""
		if !until.IsZero() {
			end = localTime(until.Add(-time.Minute), loc)
		}
		rng = localTime(since, loc) + " – " + end
	}
	counted := "human and agent events"
	if humanOnly {
		counted = "human events only"
	}
	_, _ = fmt.Fprintf(w, "%s (%s, day starts %s, %s)\n\n", rng, loc, dayStart, counted)
	rows := make([][]string, 0, len(days))
	for _, d := range days {
		row := []string{d.Label, d.Day.Format("Mon"), "-", "-", "-"}
		if d.Active() {
			row[2], row[3], row[4] = d.FirstClock(loc), d.LastClock(loc), duration(d.Span())
		}
		rows = append(rows, row)
	}
	if err := table(w, []string{"DATE", "DAY", "FIRST", "LAST", "SPAN"}, rows); err != nil {
		return err
	}
	span, active := store.WorkdayTotal(days)
	_, _ = fmt.Fprintf(w, "\ntotal: %s with activity, %s\n", plural(active, "day"), duration(span))
	return nil
}

// plural formats n with the singular or plural noun: "1 day", "3 days".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
