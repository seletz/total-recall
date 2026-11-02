package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the workday tests pin Europe/Berlin DST changes

	"github.com/seletz/total-recall/internal/model"
)

// seedWorkday writes sessions around the two Europe/Berlin DST changes of
// 2026 (2026-03-29 02:00 → 03:00 and 2026-10-25 03:00 → 02:00). Times in
// the comments are local; the day boundary is 04:00.
func seedWorkday(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:w1", Harness: "claude", NativeID: "w1", Cwd: "/w/app",
			StartedAt: ts("2026-03-28T21:00:00Z"), EndedAt: ts("2026-03-29T01:30:00Z")},
		{ID: "claude:w2", Harness: "claude", NativeID: "w2", Cwd: "/w/app",
			StartedAt: ts("2026-03-29T02:00:00Z"), EndedAt: ts("2026-03-29T10:00:00Z")},
		{ID: "claude:w3", Harness: "claude", NativeID: "w3", Cwd: "/w/docs",
			StartedAt: ts("2026-03-31T07:00:00Z"), EndedAt: ts("2026-03-31T07:00:00Z")},
		{ID: "claude:w4", Harness: "claude", NativeID: "w4", Cwd: "/w/app",
			StartedAt: ts("2026-10-24T21:00:00Z"), EndedAt: ts("2026-10-25T01:30:00Z")},
	}
	evs := []model.Event{
		// w1: Sat 03-28 22:00 CET … Sun 03-29 03:30 CEST; all before 04:00 on
		// the 29th, so one day 03-28. The clock jumps from 02:00 to 03:00 in
		// between: 5h30m by the clock, 4h30m elapsed.
		ev("claude:w1-1", "claude:w1", "2026-03-28T21:00:00Z", model.ActorHuman, model.KindMessage, "user", "a"),
		ev("claude:w1-2", "claude:w1", "2026-03-29T00:30:00Z", model.ActorAgent, model.KindMessage, "assistant", "b"),
		ev("claude:w1-3", "claude:w1", "2026-03-29T01:30:00Z", model.ActorHuman, model.KindMessage, "user", "c"),
		ev("claude:w1-4", "claude:w1", "2026-03-29T01:31:00Z", model.ActorTool, model.KindToolResult, "tool", "ignored"),
		// w2: Sun 03-29 04:00 CEST exactly on the boundary (belongs to the
		// 29th), 03:59:59 CEST just before it (belongs to the 28th), then an
		// agent event at 12:00 CEST.
		ev("claude:w2-1", "claude:w2", "2026-03-29T01:59:59Z", model.ActorAgent, model.KindMessage, "assistant", "d"),
		ev("claude:w2-2", "claude:w2", "2026-03-29T02:00:00Z", model.ActorHuman, model.KindMessage, "user", "e"),
		ev("claude:w2-3", "claude:w2", "2026-03-29T10:00:00Z", model.ActorAgent, model.KindMessage, "assistant", "f"),
		// w3: Tue 03-31 09:00 CEST, a single event: first == last.
		ev("claude:w3-1", "claude:w3", "2026-03-31T07:00:00Z", model.ActorHuman, model.KindMessage, "user", "g"),
		// w4: Sat 10-24 23:00 CEST … Sun 10-25 02:30 CET (after the clock went
		// back): 3h30m by the clock, 4h30m elapsed; one day 10-24.
		ev("claude:w4-1", "claude:w4", "2026-10-24T21:00:00Z", model.ActorHuman, model.KindMessage, "user", "h"),
		ev("claude:w4-2", "claude:w4", "2026-10-25T01:30:00Z", model.ActorAgent, model.KindMessage, "assistant", "i"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/w.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

// fmtWorkday renders a day as "label first last span sessions/human/agent",
// e.g. "2026-03-28 22:00 03:30 (+1) 4h30m0s 1/2/1"; "-" for an empty day.
func fmtWorkday(d Workday, loc *time.Location) string {
	if !d.Active() {
		return d.Label + " -"
	}
	return fmt.Sprintf("%s %s %s %s %d/%d/%d", d.Label, d.FirstClock(loc), d.LastClock(loc), d.Span(),
		d.Sessions, d.Human, d.Agent)
}

func TestWorkday(t *testing.T) {
	s, _ := openTestStore(t)
	seedWorkday(t, s)
	loc := berlin(t)
	local := func(y int, m time.Month, d, hh, mm int) time.Time {
		return time.Date(y, m, d, hh, mm, 0, 0, loc)
	}
	const dayStart = 4 * time.Hour
	tests := []struct {
		name string
		f    WorkdayFilter
		want []string
	}{
		{
			name: "spring DST night: midnight crossing, boundary exactly, elapsed span",
			f: WorkdayFilter{Since: local(2026, 3, 28, 4, 0), Until: local(2026, 3, 30, 4, 0),
				Location: loc, DayStart: dayStart},
			want: []string{
				"2026-03-28 22:00 03:59 (+1) 4h59m59s 2/2/2",
				"2026-03-29 04:00 12:00 8h0m0s 1/1/1",
			},
		},
		{
			name: "empty days are filled, single event has a zero span",
			f: WorkdayFilter{Since: local(2026, 3, 29, 4, 0), Until: local(2026, 4, 1, 4, 0),
				Location: loc, DayStart: dayStart},
			want: []string{
				"2026-03-29 04:00 12:00 8h0m0s 1/1/1",
				"2026-03-30 -",
				"2026-03-31 09:00 09:00 0s 1/1/0",
			},
		},
		{
			name: "human only drops agent events and sessions without human events",
			f: WorkdayFilter{Since: local(2026, 3, 28, 4, 0), Until: local(2026, 3, 30, 4, 0),
				Location: loc, DayStart: dayStart, HumanOnly: true},
			want: []string{
				"2026-03-28 22:00 03:30 (+1) 4h30m0s 1/2/0",
				"2026-03-29 04:00 04:00 0s 1/1/0",
			},
		},
		{
			name: "autumn DST night: clock goes back, span is elapsed time",
			f: WorkdayFilter{Since: local(2026, 10, 24, 4, 0), Until: local(2026, 10, 25, 4, 0),
				Location: loc, DayStart: dayStart},
			want: []string{"2026-10-24 23:00 02:30 (+1) 4h30m0s 1/1/1"},
		},
		{
			name: "midnight boundary attributes the night to the next day",
			f: WorkdayFilter{Since: local(2026, 3, 28, 0, 0), Until: local(2026, 3, 30, 0, 0),
				Location: loc},
			want: []string{
				"2026-03-28 22:00 22:00 0s 1/1/0",
				"2026-03-29 01:30 12:00 9h30m0s 2/2/3",
			},
		},
		{
			name: "open range covers the first to the last day with activity",
			f:    WorkdayFilter{Location: loc, DayStart: dayStart},
			want: func() []string {
				out := []string{
					"2026-03-28 22:00 03:59 (+1) 4h59m59s 2/2/2",
					"2026-03-29 04:00 12:00 8h0m0s 1/1/1",
					"2026-03-30 -",
					"2026-03-31 09:00 09:00 0s 1/1/0",
				}
				for d := local(2026, 4, 1, 0, 0); d.Before(local(2026, 10, 24, 0, 0)); d = d.AddDate(0, 0, 1) {
					out = append(out, d.Format("2006-01-02")+" -")
				}
				return append(out, "2026-10-24 23:00 02:30 (+1) 4h30m0s 1/1/1")
			}(),
		},
		{
			name: "Since inside a day reports that day from Since on",
			f: WorkdayFilter{Since: local(2026, 3, 29, 1, 0), Until: local(2026, 3, 29, 4, 0),
				Location: loc, DayStart: dayStart},
			want: []string{"2026-03-28 01:30 (+1) 03:59 (+1) 1h29m59s 2/1/2"},
		},
		{
			name: "range without activity",
			f: WorkdayFilter{Since: local(2026, 5, 4, 4, 0), Until: local(2026, 5, 6, 4, 0),
				Location: loc, DayStart: dayStart},
			want: []string{"2026-05-04 -", "2026-05-05 -"},
		},
		{
			name: "UTC location",
			f: WorkdayFilter{Since: ts("2026-03-28T04:00:00Z"), Until: ts("2026-03-30T04:00:00Z"),
				Location: time.UTC, DayStart: dayStart},
			want: []string{
				"2026-03-28 21:00 02:00 (+1) 5h0m0s 2/3/2",
				"2026-03-29 10:00 10:00 0s 1/0/1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days, err := s.Workday(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, len(days))
			for i, d := range days {
				got[i] = fmtWorkday(d, tt.f.Location)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("days =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestWorkdayEmptyStore(t *testing.T) {
	s, _ := openTestStore(t)
	days, err := s.Workday(ctx, WorkdayFilter{Location: berlin(t), DayStart: 4 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 0 {
		t.Errorf("days = %v, want none", days)
	}
}

func TestWorkdayRejectsBadDayStart(t *testing.T) {
	s, _ := openTestStore(t)
	for _, d := range []time.Duration{-time.Minute, 24 * time.Hour} {
		if _, err := s.Workday(ctx, WorkdayFilter{DayStart: d}); err == nil {
			t.Errorf("DayStart %s: no error", d)
		}
	}
}

// TestDayOf pins the attribution rule on the DST change day: the boundary
// is a wall-clock time, not an offset from midnight.
func TestDayOf(t *testing.T) {
	loc := berlin(t)
	tests := []struct {
		at       string
		dayStart time.Duration
		want     string
	}{
		{"2026-03-29T01:59:59Z", 4 * time.Hour, "2026-03-28"}, // 03:59:59 CEST
		{"2026-03-29T02:00:00Z", 4 * time.Hour, "2026-03-29"}, // 04:00:00 CEST
		{"2026-03-29T00:30:00Z", 4 * time.Hour, "2026-03-28"}, // 01:30 CET
		{"2026-03-28T23:00:00Z", 0, "2026-03-29"},             // 00:00 CET, midnight boundary
		{"2026-03-28T22:59:59Z", 0, "2026-03-28"},
		{"2026-10-25T01:30:00Z", 4 * time.Hour, "2026-10-24"},                 // 02:30 CET, after the clock went back
		{"2026-10-25T02:00:00Z", 4 * time.Hour, "2026-10-24"},                 // 03:00 CET, still before the boundary
		{"2026-10-25T03:00:00Z", 4 * time.Hour, "2026-10-25"},                 // 04:00 CET
		{"2026-06-01T09:00:00Z", 11*time.Hour + 30*time.Minute, "2026-05-31"}, // 11:00 CEST before 11:30
	}
	for _, tt := range tests {
		got := DayOf(ts(tt.at), loc, tt.dayStart)
		if got.Format("2006-01-02") != tt.want || got.Location() != loc || !got.Equal(time.Date(got.Year(), got.Month(), got.Day(), 0, 0, 0, 0, loc)) {
			t.Errorf("DayOf(%s, %s) = %v, want %s at local midnight", tt.at, tt.dayStart, got, tt.want)
		}
	}
}

// TestDayBoundary checks that the boundary keeps its wall-clock time across
// the DST change and that time of day of the given day is ignored.
func TestDayBoundary(t *testing.T) {
	loc := berlin(t)
	tests := []struct {
		day      time.Time
		dayStart time.Duration
		want     string
	}{
		{time.Date(2026, 3, 29, 0, 0, 0, 0, loc), 4 * time.Hour, "2026-03-29T04:00:00+02:00"},
		{time.Date(2026, 3, 28, 15, 0, 0, 0, loc), 4 * time.Hour, "2026-03-28T04:00:00+01:00"},
		{time.Date(2026, 10, 25, 0, 0, 0, 0, loc), 4 * time.Hour, "2026-10-25T04:00:00+01:00"},
		{time.Date(2026, 10, 25, 0, 0, 0, 0, loc), 0, "2026-10-25T00:00:00+02:00"},
	}
	for _, tt := range tests {
		if got := DayBoundary(tt.day, tt.dayStart).Format(time.RFC3339); got != tt.want {
			t.Errorf("DayBoundary(%v, %s) = %s, want %s", tt.day, tt.dayStart, got, tt.want)
		}
	}
}

func TestWorkdayTotal(t *testing.T) {
	loc := berlin(t)
	days := []Workday{
		{Day: time.Date(2026, 1, 5, 0, 0, 0, 0, loc), First: ts("2026-01-05T08:00:00Z"), Last: ts("2026-01-05T10:30:00Z"), Activity: Activity{Sessions: 1}},
		{Day: time.Date(2026, 1, 6, 0, 0, 0, 0, loc)},
		{Day: time.Date(2026, 1, 7, 0, 0, 0, 0, loc), First: ts("2026-01-07T08:00:00Z"), Last: ts("2026-01-07T08:00:00Z"), Activity: Activity{Sessions: 2}},
	}
	span, active := WorkdayTotal(days)
	if span != 2*time.Hour+30*time.Minute || active != 2 {
		t.Errorf("WorkdayTotal = %s, %d; want 2h30m, 2", span, active)
	}
	if span, active := WorkdayTotal(nil); span != 0 || active != 0 {
		t.Errorf("WorkdayTotal(nil) = %s, %d", span, active)
	}
}

// TestWorkdayClocks checks the local clock strings, including the "(+1)"
// suffix for a last activity after midnight and "(+2)" two days on.
func TestWorkdayClocks(t *testing.T) {
	loc := berlin(t)
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	tests := []struct {
		first, last string
		wantFirst   string
		wantLast    string
	}{
		{"2026-01-05T07:05:00Z", "2026-01-05T16:45:00Z", "08:05", "17:45"},
		{"2026-01-05T07:05:00Z", "2026-01-06T00:30:00Z", "08:05", "01:30 (+1)"},
		{"2026-01-05T23:30:00Z", "2026-01-07T01:00:00Z", "00:30 (+1)", "02:00 (+2)"},
	}
	for _, tt := range tests {
		d := Workday{Day: day, First: ts(tt.first), Last: ts(tt.last), Activity: Activity{Sessions: 1}}
		if got := d.FirstClock(loc); got != tt.wantFirst {
			t.Errorf("FirstClock(%s) = %q, want %q", tt.first, got, tt.wantFirst)
		}
		if got := d.LastClock(loc); got != tt.wantLast {
			t.Errorf("LastClock(%s) = %q, want %q", tt.last, got, tt.wantLast)
		}
	}
	if got := (Workday{Day: day}).FirstClock(loc); got != "" {
		t.Errorf("FirstClock of an empty day = %q, want empty", got)
	}
}
