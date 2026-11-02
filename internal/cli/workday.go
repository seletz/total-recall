package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
)

// workHours is the configured working time as the store sees it.
func workHours(cfg *config.Config) store.WorkHours {
	start, end := cfg.WorkHours()
	return store.WorkHours{Start: start, End: end, Target: cfg.Work.Target.Duration, Low: cfg.Work.DailyLow.Duration, High: cfg.Work.DailyHigh.Duration}
}

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
	// WorkHours are the core hours ("09:00-17:00") and TargetSeconds the
	// daily target on weekdays the days are measured against.
	WorkHours     string `json:"work_hours"`
	TargetSeconds int    `json:"target_seconds"`
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
	Weekend     bool   `json:"weekend"`
	// TargetSeconds is the day's target (zero on a weekend or without
	// activity), DiffSeconds the span minus the target (zero without
	// activity).
	TargetSeconds int `json:"target_seconds"`
	DiffSeconds   int `json:"diff_seconds"`
	ActivityJSON
}

// WorkdayTotalJSON sums the range.
type WorkdayTotalJSON struct {
	Days        int `json:"days"`     // days with activity
	Workdays    int `json:"workdays"` // active weekdays, the days with a target
	SpanSeconds int `json:"span_seconds"`
	// TargetSeconds is the target of the workdays, BalanceSeconds the sum
	// of the spans minus it.
	TargetSeconds  int `json:"target_seconds"`
	BalanceSeconds int `json:"balance_seconds"`
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
func workdayJSON(days []store.Workday, since, until time.Time, loc *time.Location, tz, dayStart string, humanOnly bool, wh store.WorkHours, hours string) WorkdayJSON {
	out := WorkdayJSON{
		Since: isoTime(since), Until: isoTime(until), Timezone: tz, DayStart: dayStart, HumanOnly: humanOnly,
		WorkHours: hours, TargetSeconds: int(wh.Target.Seconds()),
		Days: make([]WorkdayDayJSON, 0, len(days)),
	}
	for _, d := range days {
		diff, _ := wh.Diff(d)
		out.Days = append(out.Days, WorkdayDayJSON{
			Day: d.Label, Weekday: d.Day.Format("Mon"),
			First: isoTime(d.First), Last: isoTime(d.Last),
			FirstLocal: d.FirstClock(loc), LastLocal: d.LastClock(loc),
			SpanSeconds: int(d.Span().Seconds()), Weekend: store.IsWeekend(d.Day),
			TargetSeconds: int(wh.TargetFor(d).Seconds()), DiffSeconds: int(diff.Seconds()),
			ActivityJSON: act(d.Activity),
		})
	}
	b := wh.Balance(days)
	out.Total = WorkdayTotalJSON{
		Days: b.Active, Workdays: b.Workdays, SpanSeconds: int(b.Span.Seconds()),
		TargetSeconds: int(b.Target.Seconds()), BalanceSeconds: int(b.Balance.Seconds()),
	}
	return out
}

// writeWorkdayTable writes the range, one row per day (DATE DAY FIRST LAST
// SPAN DIFF, "-" without activity) and the total line with the balance
// against the target of the active weekdays.
func writeWorkdayTable(w io.Writer, days []store.Workday, since, until time.Time, loc *time.Location, dayStart string, humanOnly bool, wh store.WorkHours, hours string) error {
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
	_, _ = fmt.Fprintf(w, "%s (%s, day starts %s, %s)\n", rng, loc, dayStart, counted)
	_, _ = fmt.Fprintf(w, "core hours %s, target %s per weekday\n\n", hours, store.FormatSpan(wh.Target))
	rows := make([][]string, 0, len(days))
	for _, d := range days {
		row := []string{d.Label, d.Day.Format("Mon"), "-", "-", "-", "-"}
		if d.Active() {
			row[2], row[3], row[4] = d.FirstClock(loc), d.LastClock(loc), duration(d.Span())
		}
		if diff, ok := wh.Diff(d); ok {
			row[5] = store.FormatDiff(diff)
		}
		rows = append(rows, row)
	}
	if err := table(w, []string{"DATE", "DAY", "FIRST", "LAST", "SPAN", "DIFF"}, rows); err != nil {
		return err
	}
	b := wh.Balance(days)
	_, _ = fmt.Fprintf(w, "\ntotal: %s with activity, %s · target %s (%s × %s) · balance %s\n",
		plural(b.Active, "day"), store.FormatSpan(b.Span), store.FormatSpan(b.Target), plural(b.Workdays, "workday"), store.FormatSpan(wh.Target), store.FormatDiff(b.Balance))
	return nil
}

// plural formats n with the singular or plural noun: "1 day", "3 days".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
