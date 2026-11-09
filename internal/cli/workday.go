package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

// workdayReport is the output of stats --workday: the days of a range with
// the settings they were computed under.
type workdayReport struct {
	days         []store.Workday
	since, until time.Time
	loc          *time.Location
	tz, dayStart string
	humanOnly    bool
	work         store.WorkHours
	hours        string        // core hours as configured, "09:00-17:00"
	region       string        // holiday region in use
	asOf         time.Time     // today as a local midnight; later days are no workdays yet
	blockGap     time.Duration // > 0: the days carry blocks (--blocks)
}

// Range names of stats --workday --range.
var rangeNames = []string{"today", "this-week", "last-week", "this-month", "last-month"}

// WorkdayJSON is the --json output of stats --workday: the first and the
// last activity per day of the range.
type WorkdayJSON struct {
	Since     NullString `json:"since"`
	Until     NullString `json:"until"`
	Timezone  string     `json:"timezone"`
	DayStart  string     `json:"day_start"`  // HH:MM, activity before it counts for the previous day
	HumanOnly bool       `json:"human_only"` // only human events were counted
	// WorkHours are the core hours ("09:00-17:00") and TargetSeconds the
	// daily target on workdays the days are measured against.
	WorkHours     string `json:"work_hours"`
	TargetSeconds int    `json:"target_seconds"`
	// HolidayRegion is the Bundesland whose public holidays apply
	// ("Deutschland" for the nationwide set).
	HolidayRegion string `json:"holiday_region"`
	// BlockGapSeconds is the gap that ends a block; only with --blocks.
	BlockGapSeconds *int `json:"block_gap_seconds,omitempty"`
	// Days are all days of the range, in order; a day without activity has
	// empty times and zero counts.
	Days  []WorkdayDayJSON `json:"days"`
	Total WorkdayTotalJSON `json:"total"`
}

// WorkdayDayJSON is one day.
type WorkdayDayJSON struct {
	Day     string     `json:"day"`     // 2006-01-02 in local time
	Weekday string     `json:"weekday"` // Mon … Sun
	First   NullString `json:"first"`   // first activity, UTC; null without activity
	Last    NullString `json:"last"`    // last activity, UTC
	// FirstLocal and LastLocal are local clock times ("08:12"); past
	// midnight the days after Day follow in brackets ("01:30 (+1)").
	FirstLocal  NullString `json:"first_local"`
	LastLocal   NullString `json:"last_local"`
	SpanSeconds int        `json:"span_seconds"` // elapsed time between first and last
	Weekend     bool       `json:"weekend"`
	Holiday     NullString `json:"holiday"` // name of the public holiday, null on an ordinary day
	// TargetSeconds is the day's target (zero on a weekend, a holiday or
	// without activity), DiffSeconds the span minus the target (zero
	// without activity).
	TargetSeconds int `json:"target_seconds"`
	DiffSeconds   int `json:"diff_seconds"`
	// With --blocks: the runs of activity of the day (empty without
	// activity), their sum and the sum of the pauses between them; blocks
	// plus pauses make up span_seconds.
	Blocks       *[]WorkdayBlockJSON `json:"blocks,omitempty"`
	BlockSeconds *int                `json:"block_seconds,omitempty"`
	BreakSeconds *int                `json:"break_seconds,omitempty"`
	ActivityJSON
}

// WorkdayBlockJSON is a run of activity: no gap above block_gap_seconds
// inside it. A single event is a block of zero seconds.
type WorkdayBlockJSON struct {
	Start      string `json:"start"` // UTC
	End        string `json:"end"`
	StartLocal string `json:"start_local"` // "08:12"; "01:30 (+1)" past midnight
	EndLocal   string `json:"end_local"`
	Seconds    int    `json:"seconds"`
}

// WorkdayTotalJSON sums the range.
type WorkdayTotalJSON struct {
	Days int `json:"days"` // days with activity
	// Workdays are the weekdays of the range that are no holiday, up to
	// today, with or without activity; Holidays the public holidays of the
	// range, weekends included.
	Workdays    int `json:"workdays"`
	Holidays    int `json:"holidays"`
	SpanSeconds int `json:"span_seconds"`
	// TargetSeconds is the daily target × workdays, BalanceSeconds the sum
	// of the spans minus it.
	TargetSeconds  int `json:"target_seconds"`
	BalanceSeconds int `json:"balance_seconds"`
	// With --blocks: the sums of the blocks and of the pauses over all days;
	// together they are span_seconds.
	BlockSeconds *int `json:"block_seconds,omitempty"`
	BreakSeconds *int `json:"break_seconds,omitempty"`
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

// json converts the report to its JSON form.
func (r workdayReport) json() WorkdayJSON {
	out := WorkdayJSON{
		Since: NullString(isoTime(r.since)), Until: NullString(isoTime(r.until)), Timezone: r.tz, DayStart: r.dayStart, HumanOnly: r.humanOnly,
		WorkHours: r.hours, TargetSeconds: int(r.work.Target.Seconds()), HolidayRegion: r.region,
		Days: make([]WorkdayDayJSON, 0, len(r.days)),
	}
	var blockSum, breakSum time.Duration
	if r.blockGap > 0 {
		g := int(r.blockGap.Seconds())
		out.BlockGapSeconds = &g
	}
	for _, d := range r.days {
		diff, _ := r.work.Diff(d)
		day := WorkdayDayJSON{
			Day: d.Label, Weekday: d.Day.Format("Mon"),
			First: NullString(isoTime(d.First)), Last: NullString(isoTime(d.Last)),
			FirstLocal: NullString(d.FirstClock(r.loc)), LastLocal: NullString(d.LastClock(r.loc)),
			SpanSeconds: int(d.Span().Seconds()), Weekend: store.IsWeekend(d.Day), Holiday: NullString(d.Holiday),
			TargetSeconds: int(r.work.TargetFor(d).Seconds()), DiffSeconds: int(diff.Seconds()),
			ActivityJSON: act(d.Activity),
		}
		if r.blockGap > 0 {
			bl := make([]WorkdayBlockJSON, 0, len(d.Blocks))
			for _, b := range d.Blocks {
				bl = append(bl, WorkdayBlockJSON{
					Start: isoTime(b.Start), End: isoTime(b.End),
					StartLocal: d.Clock(b.Start, r.loc), EndLocal: d.Clock(b.End, r.loc), Seconds: int(b.Duration().Seconds()),
				})
			}
			bs, ps := int(d.BlockSum().Seconds()), int(d.BreakSum().Seconds())
			day.Blocks, day.BlockSeconds, day.BreakSeconds = &bl, &bs, &ps
			blockSum += d.BlockSum()
			breakSum += d.BreakSum()
		}
		out.Days = append(out.Days, day)
	}
	b := r.work.Balance(r.days, r.asOf)
	out.Total = WorkdayTotalJSON{
		Days: b.Active, Workdays: b.Workdays, Holidays: b.Holidays, SpanSeconds: int(b.Span.Seconds()),
		TargetSeconds: int(b.Target.Seconds()), BalanceSeconds: int(b.Balance.Seconds()),
	}
	if r.blockGap > 0 {
		bs, ps := int(blockSum.Seconds()), int(breakSum.Seconds())
		out.Total.BlockSeconds, out.Total.BreakSeconds = &bs, &ps
	}
	return out
}

// table writes the range, one row per day (DATE DAY FIRST LAST SPAN DIFF
// HOLIDAY, "-" where there is nothing, the holiday name on a public holiday) and
// the total line with the balance against the target of the workdays.
func (r workdayReport) table(w io.Writer) error {
	rng := "all time"
	if !r.since.IsZero() || !r.until.IsZero() {
		end := ""
		if !r.until.IsZero() {
			end = localTime(r.until.Add(-time.Minute), r.loc)
		}
		rng = localTime(r.since, r.loc) + " – " + end
	}
	counted := "human and agent events"
	if r.humanOnly {
		counted = "human events only"
	}
	_, _ = fmt.Fprintf(w, "%s (%s, day starts %s, %s)\n", rng, r.loc, r.dayStart, counted)
	_, _ = fmt.Fprintf(w, "core hours %s, target %s per workday, holidays %s\n", r.hours, store.FormatSpan(r.work.Target), r.region)
	if r.blockGap > 0 {
		_, _ = fmt.Fprintf(w, "blocks end at a gap above %s\n", store.FormatSpan(r.blockGap))
	}
	_, _ = fmt.Fprintln(w)
	rows := make([][]string, 0, len(r.days))
	for _, d := range r.days {
		row := []string{d.Label, d.Day.Format("Mon"), "-", "-", "-", "-", "-"}
		if d.IsHoliday() {
			row[6] = d.Holiday
		}
		if d.Active() {
			row[2], row[3], row[4] = d.FirstClock(r.loc), d.LastClock(r.loc), duration(d.Span())
		}
		if diff, ok := r.work.Diff(d); ok {
			row[5] = store.FormatDiff(diff)
		}
		if r.blockGap > 0 {
			row = append(row, "-", "-", "-")
			if d.Active() {
				parts := make([]string, 0, len(d.Blocks))
				for _, b := range d.Blocks {
					parts = append(parts, d.Clock(b.Start, r.loc)+"-"+d.Clock(b.End, r.loc))
				}
				row[7], row[8], row[9] = strings.Join(parts, " "), duration(d.BlockSum()), duration(d.BreakSum())
			}
		}
		rows = append(rows, row)
	}
	header := []string{"DATE", "DAY", "FIRST", "LAST", "SPAN", "DIFF", "HOLIDAY"}
	if r.blockGap > 0 {
		header = append(header, "BLOCKS", "WORK", "BREAKS")
	}
	if err := table(w, header, rows); err != nil {
		return err
	}
	b := r.work.Balance(r.days, r.asOf)
	_, _ = fmt.Fprintf(w, "\ntotal: %s with activity, %s · target %s (%s × %s) · balance %s",
		plural(b.Active, "day"), store.FormatSpan(b.Span), store.FormatSpan(b.Target), plural(b.Workdays, "workday"), store.FormatSpan(r.work.Target), store.FormatDiff(b.Balance))
	if b.Holidays > 0 {
		_, _ = fmt.Fprintf(w, " · %s", plural(b.Holidays, "holiday"))
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

// plural formats n with the singular or plural noun: "1 day", "3 days".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
