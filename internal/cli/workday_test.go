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
		{"HumanOnly", []string{"--range", "last-week", "--human-only"}},
		{"Empty", []string{"--since", "2026-03-02", "--until", "2026-03-08"}},
		{"ThisWeekJSON", []string{"--range", "this-week", "--json"}},
		{"LastWeekHumanOnlyJSON", []string{"--range", "last-week", "--human-only", "--json"}},
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
	if d := out.Days[0]; d.Day != "2025-12-31" || d.FirstLocal != "23:40" || d.LastLocal != "00:05 (+1)" || d.SpanSeconds != 25*60+5 {
		t.Errorf("default day 0 = %+v", d)
	}
	if d := out.Days[1]; d.Day != "2026-01-01" || d.Sessions != 0 || d.First != "" || d.FirstLocal != "" {
		t.Errorf("default day 1 = %+v", d)
	}
	if out.Total.Days != 1 || out.Total.SpanSeconds != 25*60+5 {
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
	if d := out.Days[1]; d.FirstLocal != "00:05" || d.LastLocal != "00:05" || d.Human != 1 || d.Agent != 1 {
		t.Errorf("midnight day 1 = %+v", d)
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
