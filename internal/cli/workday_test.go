package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// activityNow is a Sunday noon at the end of the Activity fixture, so the
// relative ranges of --range fall into it: this week is 2026-W02, last week
// 2026-W01, this month January 2026, last month December 2025.
var activityNow = time.Date(2026, 1, 11, 12, 0, 0, 0, time.UTC)

// TestStatsWorkday compares the text and JSON output of --workday with
// golden files (go test ./internal/cli -run TestStatsWorkday -update).
func TestStatsWorkday(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"ThisWeek", []string{"--range", "this-week"}},
		{"LastWeek", []string{"--range", "last-week"}},
		{"ThisMonth", []string{"--range", "this-month"}},
		{"LastMonth", []string{"--range", "last-month"}},
		{"Today", []string{"--range", "today"}},
		{"DefaultIsThisWeek", nil},
		{"SinceUntil", []string{"--since", "2025-12-29", "--until", "2026-01-04"}},
		{"Month", []string{"--month", "2025-12"}},
		{"AllEvents", []string{"--range", "last-week", "--all-events"}},
		{"HumanOnlyDeprecated", []string{"--range", "last-week", "--human-only"}},
		{"Empty", []string{"--since", "2026-03-02", "--until", "2026-03-08"}},
		{"ThisWeekJSON", []string{"--range", "this-week", "--json"}},
		{"LastWeekAllEventsJSON", []string{"--range", "last-week", "--all-events", "--json"}},
		{"EmptyJSON", []string{"--since", "2026-03-02", "--until", "2026-03-08", "--json"}},
	}
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runActivity(t, e, db, append([]string{"--workday"}, tt.args...)...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			golden.RequireEqual(t, []byte(r.stdout))
		})
	}
}

// TestStatsWorkdayDayStart checks the day rule: with the default 04:00 the
// session A5 (2025-12-31 23:40 – 2026-01-01 00:05) is one day ending at
// "00:05 (+1)"; with day_start = "00:00" it is split over two days.
func TestStatsWorkdayDayStart(t *testing.T) {
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	args := []string{"--workday", "--since", "2025-12-31", "--until", "2026-01-01", "--json"}

	out := decode[WorkdayJSON](t, runActivity(t, e, db, args...).stdout)
	if out.DayStart != "04:00" || len(out.Days) != 2 {
		t.Fatalf("default: day_start=%q days=%d", out.DayStart, len(out.Days))
	}
	if d := out.Days[0]; d.Day != "2025-12-31" || d.FirstLocal != "23:40" || d.LastLocal != "00:05 (+1)" || d.SpanSeconds != 25*60 {
		t.Errorf("default day 0 = %+v", d)
	}
	if d := out.Days[1]; d.Day != "2026-01-01" || d.Sessions != 0 || d.First != "" || d.FirstLocal != "" {
		t.Errorf("default day 1 = %+v", d)
	}
	if out.Total.Days != 1 || out.Total.SpanSeconds != 25*60 {
		t.Errorf("default total = %+v", out.Total)
	}

	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	// Top-level keys go before the first table.
	if err := os.WriteFile(e.configPath, append([]byte("day_start = \"00:00\"\n"), cfg...), 0o644); err != nil {
		t.Fatal(err)
	}
	out = decode[WorkdayJSON](t, runActivity(t, e, db, args...).stdout)
	if out.DayStart != "00:00" || len(out.Days) != 2 {
		t.Fatalf("midnight: day_start=%q days=%d", out.DayStart, len(out.Days))
	}
	if d := out.Days[0]; d.FirstLocal != "23:40" || d.LastLocal != "23:40" || d.Sessions != 1 {
		t.Errorf("midnight day 0 = %+v", d)
	}
	if d := out.Days[1]; d.FirstLocal != "00:05" || d.LastLocal != "00:05" || d.Human != 1 || d.Agent != 0 || d.Holiday != "Neujahr" {
		t.Errorf("midnight day 1 = %+v", d)
	}
}

// TestStatsWorkdayTarget checks the target fields of the JSON: a weekday
// is measured against 8h, a weekend and a holiday have no target and their
// span is the difference, the total balances the spans against every
// workday of the range (this week: five weekdays minus Epiphanias). Only
// human messages count, so a day with one prompt has a span of zero.
func TestStatsWorkdayTarget(t *testing.T) {
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	out := decode[WorkdayJSON](t, runActivity(t, e, db, "--workday", "--range", "this-week", "--json").stdout)
	if out.WorkHours != "09:00-17:00" || out.TargetSeconds != 8*3600 {
		t.Errorf("work = %q, %d", out.WorkHours, out.TargetSeconds)
	}
	byDay := map[string]WorkdayDayJSON{}
	for _, d := range out.Days {
		byDay[d.Weekday] = d
	}
	if d := byDay["Mon"]; d.Weekend || d.TargetSeconds != 8*3600 || d.DiffSeconds != -8*3600 {
		t.Errorf("Mon = %+v", d)
	}
	if d := byDay["Tue"]; d.Weekend || d.Holiday != "Epiphanias" || d.TargetSeconds != 0 || d.DiffSeconds != 0 {
		t.Errorf("Tue (holiday, inactive) = %+v", d)
	}
	if d := byDay["Wed"]; d.Weekend || d.Holiday != "" || d.TargetSeconds != 0 || d.DiffSeconds != 0 {
		t.Errorf("Wed (inactive) = %+v", d)
	}
	if d := byDay["Sat"]; !d.Weekend || d.TargetSeconds != 0 || d.DiffSeconds != 0 {
		t.Errorf("Sat (inactive) = %+v", d)
	}
	if d := byDay["Sun"]; !d.Weekend || d.TargetSeconds != 0 || d.DiffSeconds != 0 {
		t.Errorf("Sun = %+v", d)
	}
	if out.HolidayRegion != "Baden-Württemberg" {
		t.Errorf("holiday_region = %q", out.HolidayRegion)
	}
	want := WorkdayTotalJSON{Days: 3, Workdays: 4, Holidays: 1, SpanSeconds: 1500, TargetSeconds: 32 * 3600, BalanceSeconds: 1500 - 32*3600}
	if out.Total != want {
		t.Errorf("total = %+v, want %+v", out.Total, want)
	}

	// A different target and core hours from the config.
	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append(cfg, []byte("\n[work]\nhours = \"08:00-16:00\"\ntarget = \"7h\"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := runActivity(t, e, db, "--workday", "--range", "this-week")
	if !strings.Contains(r.stdout, "core hours 08:00-16:00, target 7h00m per workday") || !strings.Contains(r.stdout, "-7h00m") {
		t.Errorf("custom work hours not applied:\n%s", r.stdout)
	}
}

func TestStatsWorkdayErrors(t *testing.T) {
	e := newTestEnv(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad range", []string{"--workday", "--range", "yesterday"}, "--range"},
		{"range without workday", []string{"--range", "this-week"}, "--workday"},
		{"range and since", []string{"--workday", "--range", "this-week", "--since", "2026-01-01"}, "either"},
		{"range and month", []string{"--workday", "--range", "this-week", "--month", "2026-01"}, "either"},
		{"workday and period", []string{"--workday", "--period", "week"}, "--period"},
		{"workday and by", []string{"--workday", "--by", "project"}, "--by"},
		{"human only without workday", []string{"--human-only"}, "--workday"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(t, "", append([]string{"stats"}, tt.args...)...)
			if r.code == 0 || !strings.Contains(r.stderr, tt.want) {
				t.Errorf("%v = exit %d, stderr %q; want failure mentioning %q", tt.args, r.code, r.stderr, tt.want)
			}
		})
	}
}

// TestRangeBounds pins the relative ranges under the 04:00 day rule, with
// now shortly after midnight: "today" is still the previous calendar day.
func TestRangeBounds(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	// Tuesday 2026-01-06 01:00 local.
	now := time.Date(2026, 1, 6, 1, 0, 0, 0, loc)
	const dayStart = 4 * time.Hour
	tests := []struct {
		name         string
		since, until string
	}{
		{"today", "2026-01-05T04:00:00+01:00", "2026-01-06T04:00:00+01:00"},
		{"this-week", "2026-01-05T04:00:00+01:00", "2026-01-12T04:00:00+01:00"},
		{"last-week", "2025-12-29T04:00:00+01:00", "2026-01-05T04:00:00+01:00"},
		{"this-month", "2026-01-01T04:00:00+01:00", "2026-02-01T04:00:00+01:00"},
		{"last-month", "2025-12-01T04:00:00+01:00", "2026-01-01T04:00:00+01:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, until, err := rangeBounds(tt.name, now, loc, dayStart)
			if err != nil {
				t.Fatal(err)
			}
			if got := since.Format(time.RFC3339); got != tt.since {
				t.Errorf("since = %s, want %s", got, tt.since)
			}
			if got := until.Format(time.RFC3339); got != tt.until {
				t.Errorf("until = %s, want %s", got, tt.until)
			}
		})
	}
	if _, _, err := rangeBounds("yesterday", now, loc, dayStart); err == nil {
		t.Error("yesterday: no error")
	}
}

// TestWorkdayBounds checks --since/--until under the day rule: dates mean
// whole days from the boundary, times are taken as they are.
func TestWorkdayBounds(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	const dayStart = 4 * time.Hour
	tests := []struct {
		sinceArg, untilArg, month string
		since, until              string
	}{
		{"2026-01-05", "2026-01-11", "", "2026-01-05T04:00:00+01:00", "2026-01-12T04:00:00+01:00"},
		{"2026-01-05T10:00", "2026-01-05T12:00", "", "2026-01-05T10:00:00+01:00", "2026-01-05T12:00:00+01:00"},
		{"2026-01-05", "", "", "2026-01-05T04:00:00+01:00", ""},
		{"", "2026-01-05", "", "", "2026-01-06T04:00:00+01:00"},
		{"", "", "2025-12", "2025-12-01T04:00:00+01:00", "2026-01-01T04:00:00+01:00"},
	}
	for _, tt := range tests {
		since, until, err := workdayBounds(tt.month, tt.sinceArg, tt.untilArg, loc, dayStart)
		if err != nil {
			t.Fatal(err)
		}
		f := func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(time.RFC3339)
		}
		if f(since) != tt.since || f(until) != tt.until {
			t.Errorf("workdayBounds(%q, %q, %q) = %s, %s; want %s, %s", tt.month, tt.sinceArg, tt.untilArg, f(since), f(until), tt.since, tt.until)
		}
	}
}

// TestStatsWorkdayBundesland checks the holiday region: Berlin has no
// Epiphanias, so this week has five workdays; an unknown name falls back
// to the nationwide holidays; the running month counts workdays up to
// today only (2026-01-11 is a Sunday: Jan 2, 5, 7, 8, 9 after the two
// holidays).
func TestStatsWorkdayBundesland(t *testing.T) {
	tests := []struct {
		name       string
		bundesland string
		region     string
		workdays   int
		holiday    string // of Tue 2026-01-06
	}{
		{"Berlin", "Berlin", "Berlin", 5, ""},
		{"unknown", "Atlantis", "Deutschland", 5, ""},
		{"spelling", "bayern", "Bayern", 4, "Epiphanias"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.now = activityNow
			_, db := fixturedb.Activity(t)
			cfg, err := os.ReadFile(e.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(e.configPath, append(cfg, []byte("\n[work]\nbundesland = \""+tt.bundesland+"\"\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			out := decode[WorkdayJSON](t, runActivity(t, e, db, "--workday", "--range", "this-week", "--json").stdout)
			if out.HolidayRegion != tt.region || out.Total.Workdays != tt.workdays || out.Days[1].Holiday != tt.holiday {
				t.Errorf("region %q, workdays %d, Tue holiday %q; want %q, %d, %q", out.HolidayRegion, out.Total.Workdays, out.Days[1].Holiday, tt.region, tt.workdays, tt.holiday)
			}
			r := runActivity(t, e, db, "--workday", "--range", "this-week")
			if !strings.Contains(r.stdout, "holidays "+tt.region) {
				t.Errorf("region missing in the header:\n%s", r.stdout)
			}
		})
	}
}

// TestStatsWorkdayRunningMonth checks that the running month counts the
// workdays up to today only: as of Sunday 2026-01-11 January has Jan 2,
// 5, 7, 8 and 9 (Neujahr and Epiphanias are holidays), not the 21
// workdays of the whole month.
func TestStatsWorkdayRunningMonth(t *testing.T) {
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	out := decode[WorkdayJSON](t, runActivity(t, e, db, "--workday", "--range", "this-month", "--json").stdout)
	if out.Total.Workdays != 5 || out.Total.Holidays != 2 || out.Total.TargetSeconds != 5*8*3600 {
		t.Errorf("total = %+v", out.Total)
	}
	out = decode[WorkdayJSON](t, runActivity(t, e, db, "--workday", "--range", "last-month", "--json").stdout)
	if out.Total.Workdays != 21 || out.Total.Holidays != 2 {
		t.Errorf("last month total = %+v", out.Total)
	}
}

// TestStatsWorkdayCount checks the counting modes: human only by default,
// --all-events for everything, the deprecated --human-only still accepted,
// and [work] count = "all" as the default with --human-only overriding it.
func TestStatsWorkdayCount(t *testing.T) {
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	tests := []struct {
		name      string
		config    string
		args      []string
		humanOnly bool
		span      int // of Mon 2026-01-05: one prompt and one reply 5s later
	}{
		{"default", "", nil, true, 0},
		{"all events", "", []string{"--all-events"}, false, 5},
		{"deprecated human-only", "", []string{"--human-only"}, true, 0},
		{"config all", "\n[work]\ncount = \"all\"\n", nil, false, 5},
		{"config all, human-only flag", "\n[work]\ncount = \"all\"\n", []string{"--human-only"}, true, 0},
		{"config human, all-events flag", "\n[work]\ncount = \"human\"\n", []string{"--all-events"}, false, 5},
	}
	base, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(e.configPath, append(append([]byte{}, base...), []byte(tt.config)...), 0o644); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--workday", "--range", "this-week", "--json"}, tt.args...)
			out := decode[WorkdayJSON](t, runActivity(t, e, db, args...).stdout)
			if out.HumanOnly != tt.humanOnly || out.Days[0].SpanSeconds != tt.span {
				t.Errorf("human_only = %v, Mon span = %d; want %v, %d", out.HumanOnly, out.Days[0].SpanSeconds, tt.humanOnly, tt.span)
			}
		})
	}
	r := runActivity(t, e, db, "--workday", "--human-only")
	if !strings.Contains(r.stderr, "deprecated") {
		t.Errorf("--human-only without a deprecation note: %q", r.stderr)
	}
}
