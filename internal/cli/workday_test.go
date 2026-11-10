package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
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
		{"blocks without workday", []string{"--blocks"}, "--workday"},
		{"blocks with worktime", []string{"--worktime", "--blocks"}, "--workday"},
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
			if out.HolidayRegion != tt.region || out.Total.Workdays != tt.workdays || string(out.Days[1].Holiday) != tt.holiday {
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

// TestStatsWorkdayBlocks compares the output of --workday --blocks with
// golden files.
func TestStatsWorkdayBlocks(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"LastWeek", []string{"--range", "last-week", "--blocks"}},
		{"LastWeekAllEvents", []string{"--range", "last-week", "--blocks", "--all-events"}},
		{"LastWeekJSON", []string{"--range", "last-week", "--blocks", "--json"}},
		{"EmptyJSON", []string{"--since", "2026-03-02", "--until", "2026-03-08", "--blocks", "--json"}},
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

// TestStatsWorkdayBlocksOptional checks that the block fields are absent
// without --blocks (the JSON of a plain --workday does not change).
func TestStatsWorkdayBlocksOptional(t *testing.T) {
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	r := runActivity(t, e, db, "--workday", "--range", "last-week", "--json")
	for _, key := range []string{"blocks", "block_seconds", "break_seconds", "block_gap_seconds"} {
		if strings.Contains(r.stdout, `"`+key+`"`) {
			t.Errorf("plain --workday JSON has %q", key)
		}
	}
}

// TestStatsWorkdayBlocksSums is the sum invariant against the real --json
// output of the demo archive, for several thresholds and both counting
// modes: per day the blocks add up to block_seconds, blocks plus breaks to
// span_seconds, the blocks are ordered, apart by more than the threshold
// and bounded by first and last; the total adds up its days; the days
// match a plain --workday run of the same range.
func TestStatsWorkdayBlocksSums(t *testing.T) {
	_, db := fixturedb.Demo(t)
	for _, gap := range []string{"1m", "30m", "2h", "24h"} {
		for _, all := range []bool{false, true} {
			t.Run(gap+map[bool]string{false: "", true: "/all-events"}[all], func(t *testing.T) {
				e := newTestEnv(t)
				e.now = demo.Now
				e.home = demo.Home
				appendConfig(t, e, "\n[work]\nblock_gap = \""+gap+"\"\n")
				args := []string{"--workday", "--month", "2026-10", "--json"}
				if all {
					args = append(args, "--all-events")
				}
				plain := decode[WorkdayJSON](t, runActivity(t, e, db, args...).stdout)
				r := runActivity(t, e, db, append(args, "--blocks")...)
				if r.code != 0 {
					t.Fatalf("exit %d: %s", r.code, r.stderr)
				}
				out := decode[WorkdayJSON](t, r.stdout)
				gapDur, _ := time.ParseDuration(gap)
				if out.BlockGapSeconds == nil || *out.BlockGapSeconds != int(gapDur.Seconds()) {
					t.Fatalf("block_gap_seconds = %v, want %v", out.BlockGapSeconds, gapDur.Seconds())
				}
				if len(out.Days) != len(plain.Days) || len(out.Days) == 0 {
					t.Fatalf("days = %d, plain %d", len(out.Days), len(plain.Days))
				}
				var sumBlocks, sumBreaks, sumSpan, nBlocks, nEvents int
				for i, d := range out.Days {
					if d.SpanSeconds != plain.Days[i].SpanSeconds || d.First != plain.Days[i].First || d.Last != plain.Days[i].Last {
						t.Errorf("%s: differs from plain --workday", d.Day)
					}
					if d.Blocks == nil || d.BlockSeconds == nil || d.BreakSeconds == nil {
						t.Fatalf("%s: block fields missing: %+v", d.Day, d)
					}
					blocks := *d.Blocks
					if (d.First == "") != (len(blocks) == 0) {
						t.Errorf("%s: first=%q with %d blocks", d.Day, d.First, len(blocks))
					}
					var sum, breaks, events int
					for j, b := range blocks {
						events += b.Events
						if b.Events < 1 || b.Single != (b.Events == 1) || (b.Single && b.Seconds != 0) {
							t.Errorf("%s block %d: events %d, single %v, seconds %d", d.Day, j, b.Events, b.Single, b.Seconds)
						}
						if b.Seconds < 0 {
							t.Errorf("%s block %d: negative", d.Day, j)
						}
						bs, _ := time.Parse(time.RFC3339, b.Start)
						be, _ := time.Parse(time.RFC3339, b.End)
						if int(be.Sub(bs).Seconds()) != b.Seconds {
							t.Errorf("%s block %d: seconds %d != end-start", d.Day, j, b.Seconds)
						}
						if j == 0 && b.Start != string(d.First) {
							t.Errorf("%s: first block starts %s, first activity %s", d.Day, b.Start, d.First)
						}
						if j == len(blocks)-1 && b.End != string(d.Last) {
							t.Errorf("%s: last block ends %s, last activity %s", d.Day, b.End, d.Last)
						}
						if j > 0 {
							pe, _ := time.Parse(time.RFC3339, blocks[j-1].End)
							g := int(bs.Sub(pe).Seconds())
							if g <= int(gapDur.Seconds()) {
								t.Errorf("%s block %d: only %ds after the previous one, threshold %s", d.Day, j, g, gap)
							}
							breaks += g
						}
						sum += b.Seconds
					}
					if sum != *d.BlockSeconds || breaks != *d.BreakSeconds || sum+breaks != d.SpanSeconds {
						t.Errorf("%s: blocks %d + breaks %d vs block_seconds %d, break_seconds %d, span %d", d.Day, sum, breaks, *d.BlockSeconds, *d.BreakSeconds, d.SpanSeconds)
					}
					if events != d.Human+d.Agent {
						t.Errorf("%s: blocks have %d events, the day counts %d (human %d, agent %d)", d.Day, events, d.Human+d.Agent, d.Human, d.Agent)
					}
					nEvents += events
					sumBlocks, sumBreaks, sumSpan, nBlocks = sumBlocks+sum, sumBreaks+breaks, sumSpan+d.SpanSeconds, nBlocks+len(blocks)
				}
				if out.Total.BlockSeconds == nil || out.Total.BreakSeconds == nil {
					t.Fatal("total block fields missing")
				}
				if *out.Total.BlockSeconds != sumBlocks || *out.Total.BreakSeconds != sumBreaks || sumBlocks+sumBreaks != out.Total.SpanSeconds || sumSpan != out.Total.SpanSeconds {
					t.Errorf("total: block %d break %d span %d; days add up to %d + %d, span %d", *out.Total.BlockSeconds, *out.Total.BreakSeconds, out.Total.SpanSeconds, sumBlocks, sumBreaks, sumSpan)
				}
				if nBlocks == 0 || nEvents == 0 {
					t.Error("demo month has no blocks")
				}
			})
		}
	}
}

// TestStatsWorkdayBlocksFractional: with fractional seconds in the time
// stamps (the real archive has milliseconds) the whole seconds in the JSON
// still add up: per day the seconds of the blocks are block_seconds, blocks
// plus breaks are span_seconds, and the total is the sum of its days.
func TestStatsWorkdayBlocksFractional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frac.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var sessions []model.Session
	var evs []model.Event
	n := 0
	for _, at := range []string{
		"2026-03-09T08:00:00.900Z", "2026-03-09T08:00:01.800Z", "2026-03-09T09:00:00.700Z", "2026-03-09T09:00:02.600Z",
		"2026-03-09T11:00:00.400Z", "2026-03-09T11:00:00.500Z", "2026-03-09T11:00:00.600Z",
		"2026-03-10T08:00:00.950Z", "2026-03-10T08:00:01.900Z", "2026-03-10T10:00:00.100Z", "2026-03-10T10:00:00.990Z",
	} {
		n++
		id := fmt.Sprintf("claude:f%d", n)
		tm, _ := time.Parse(time.RFC3339, at)
		sessions = append(sessions, model.Session{ID: id, Harness: "claude", NativeID: fmt.Sprintf("f%d", n), Cwd: "/w/app", StartedAt: tm, EndedAt: tm})
		evs = append(evs, model.Event{ID: id + "-1", SessionID: id, TS: tm, Actor: model.ActorHuman, Kind: model.KindMessage, Role: "user", Text: "x", SourcePath: "/src/f.jsonl", Raw: `{"x":1}`})
	}
	if err := st.WriteBatch(t.Context(), store.SourceState{Path: "/src/f.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	e := newTestEnv(t)
	e.now = time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)
	r := runActivity(t, e, path, "--workday", "--since", "2026-03-09", "--until", "2026-03-11", "--blocks", "--json")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	out := decode[WorkdayJSON](t, r.stdout)
	var sumBlocks, sumBreaks, sumSpan int
	for _, d := range out.Days {
		if d.Blocks == nil {
			continue
		}
		sum := 0
		for _, b := range *d.Blocks {
			sum += b.Seconds
		}
		if sum != *d.BlockSeconds || *d.BlockSeconds+*d.BreakSeconds != d.SpanSeconds {
			t.Errorf("%s: blocks %d, block_seconds %d, break_seconds %d, span %d", d.Day, sum, *d.BlockSeconds, *d.BreakSeconds, d.SpanSeconds)
		}
		sumBlocks, sumBreaks, sumSpan = sumBlocks+*d.BlockSeconds, sumBreaks+*d.BreakSeconds, sumSpan+d.SpanSeconds
	}
	tot := out.Total
	if sumSpan == 0 || *tot.BlockSeconds != sumBlocks || *tot.BreakSeconds != sumBreaks || tot.SpanSeconds != sumSpan ||
		*tot.BlockSeconds+*tot.BreakSeconds != tot.SpanSeconds || tot.BalanceSeconds != tot.SpanSeconds-tot.TargetSeconds {
		t.Errorf("total: block %d break %d span %d balance %d target %d; days: %d + %d, span %d",
			*tot.BlockSeconds, *tot.BreakSeconds, tot.SpanSeconds, tot.BalanceSeconds, tot.TargetSeconds, sumBlocks, sumBreaks, sumSpan)
	}
}
