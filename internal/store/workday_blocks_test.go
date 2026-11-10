package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// seedBlocks writes human events (UTC == the zone of the tests) around the
// block threshold, midnight and the day boundary (04:00):
//
//	03-02 07:48 08:10 08:40 09:10 (gaps up to exactly 30m: one block) | 10:30 10:50 (gap 80m: new block)
//	03-03 22:00 22:20 22:50 | 00:10 00:30 (+1, gap 70m: break across midnight) | 03:59 (+1, still 03-03)
//	03-04 04:00 (exactly the boundary) | 04:20
//	03-05 10:00 (single event)
//	03-06 an agent event at 09:00 only (counts with all events)
func seedBlocks(t *testing.T, s *Store) {
	t.Helper()
	var sessions []model.Session
	var evs []model.Event
	add := func(i int, at string, actor model.Actor) {
		id := fmt.Sprintf("claude:b%d", i)
		sessions = append(sessions, model.Session{ID: id, Harness: "claude", NativeID: fmt.Sprintf("b%d", i), Cwd: "/w/app", StartedAt: ts(at), EndedAt: ts(at)})
		kind, role := model.KindMessage, "user"
		if actor == model.ActorAgent {
			role = "assistant"
		}
		evs = append(evs, ev(id+"-1", id, at, actor, kind, role, "x"))
	}
	n := 0
	for _, a := range []string{
		"2026-03-02T07:48:00Z", "2026-03-02T08:10:00Z", "2026-03-02T08:40:00Z", "2026-03-02T09:10:00Z", "2026-03-02T10:30:00Z", "2026-03-02T10:50:00Z",
		"2026-03-03T22:00:00Z", "2026-03-03T22:20:00Z", "2026-03-03T22:50:00Z", "2026-03-04T00:10:00Z", "2026-03-04T00:30:00Z", "2026-03-04T03:59:00Z",
		"2026-03-04T04:00:00Z", "2026-03-04T04:20:00Z",
		"2026-03-05T10:00:00Z",
	} {
		n++
		add(n, a, model.ActorHuman)
	}
	n++
	add(n, "2026-03-06T09:00:00Z", model.ActorAgent)
	// Two events with the same time are one block of two events, not a single.
	n++
	add(n, "2026-03-07T12:00:00Z", model.ActorHuman)
	n++
	add(n, "2026-03-07T12:00:00Z", model.ActorHuman)
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/b.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func fmtBlocks(d Workday) string {
	var b []string
	for _, bl := range d.Blocks {
		one := ""
		if bl.Single() {
			one = " single"
		}
		b = append(b, fmt.Sprintf("%s-%s %s n=%d%s", bl.Start.UTC().Format("15:04"), bl.End.UTC().Format("15:04"), bl.Duration(), bl.Events, one))
	}
	return fmt.Sprintf("%s [%s] sum=%s breaks=%s", d.Label, strings.Join(b, ", "), d.BlockSum(), d.BreakSum())
}

func TestWorkdayBlocks(t *testing.T) {
	s, _ := openTestStore(t)
	seedBlocks(t, s)
	day := func(d int) time.Time { return time.Date(2026, 3, d, 4, 0, 0, 0, time.UTC) }
	tests := []struct {
		name string
		f    WorkdayFilter
		want []string
	}{
		{
			name: "gap at the threshold stays in the block, above it breaks",
			f:    WorkdayFilter{Since: day(2), Until: day(3), Location: time.UTC, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute, HumanOnly: true},
			want: []string{"2026-03-02 [07:48-09:10 1h22m0s n=4, 10:30-10:50 20m0s n=2] sum=1h42m0s breaks=1h20m0s"},
		},
		{
			name: "break across midnight, day belongs to the previous date until day_start",
			f:    WorkdayFilter{Since: day(3), Until: day(4), Location: time.UTC, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute, HumanOnly: true},
			want: []string{"2026-03-03 [22:00-22:50 50m0s n=3, 00:10-00:30 20m0s n=2, 03:59-03:59 0s n=1 single] sum=1h10m0s breaks=4h49m0s"},
		},
		{
			name: "event on the boundary starts the next day",
			f:    WorkdayFilter{Since: day(4), Until: day(5), Location: time.UTC, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute, HumanOnly: true},
			want: []string{"2026-03-04 [04:00-04:20 20m0s n=2] sum=20m0s breaks=0s"},
		},
		{
			name: "single event is a zero block; a larger threshold merges the day",
			f:    WorkdayFilter{Since: day(2), Until: day(6), Location: time.UTC, DayStart: 4 * time.Hour, BlockGap: 3 * time.Hour, HumanOnly: true},
			want: []string{
				"2026-03-02 [07:48-10:50 3h2m0s n=6] sum=3h2m0s breaks=0s",
				"2026-03-03 [22:00-00:30 2h30m0s n=5, 03:59-03:59 0s n=1 single] sum=2h30m0s breaks=3h29m0s",
				"2026-03-04 [04:00-04:20 20m0s n=2] sum=20m0s breaks=0s",
				"2026-03-05 [10:00-10:00 0s n=1 single] sum=0s breaks=0s",
			},
		},
		{
			name: "human only skips the agent day, all events counts it",
			f:    WorkdayFilter{Since: day(6), Until: day(7), Location: time.UTC, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute},
			want: []string{"2026-03-06 [09:00-09:00 0s n=1 single] sum=0s breaks=0s"},
		},
		{
			name: "two events with the same time are one block of two, not a single",
			f:    WorkdayFilter{Since: day(7), Until: day(8), Location: time.UTC, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute, HumanOnly: true},
			want: []string{"2026-03-07 [12:00-12:00 0s n=2] sum=0s breaks=0s"},
		},
		{
			name: "no BlockGap, no blocks",
			f:    WorkdayFilter{Since: day(5), Until: day(6), Location: time.UTC, DayStart: 4 * time.Hour, HumanOnly: true},
			want: []string{"2026-03-05 [] sum=0s breaks=0s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days, err := s.Workday(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, d := range days {
				got = append(got, fmtBlocks(d))
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			// Invariant: the events of the blocks are the counted events of
			// the day; a block is single exactly when it has one event.
			for _, d := range days {
				if len(d.Blocks) == 0 {
					continue
				}
				n := 0
				for _, b := range d.Blocks {
					n += b.Events
					if b.Single() != (b.Events == 1) {
						t.Errorf("%s: block with %d events, single=%v", d.Label, b.Events, b.Single())
					}
				}
				if n != d.Human+d.Agent {
					t.Errorf("%s: blocks have %d events, the day counts %d", d.Label, n, d.Human+d.Agent)
				}
			}
			// Invariant: blocks plus breaks make up the span.
			for _, d := range days {
				if len(d.Blocks) > 0 && d.BlockSum()+d.BreakSum() != d.Span() {
					t.Errorf("%s: blocks %s + breaks %s != span %s", d.Label, d.BlockSum(), d.BreakSum(), d.Span())
				}
			}
		})
	}
}

// TestWorkdayBlockSecondsAddUp: with fractional seconds in the time stamps
// the whole seconds of the blocks add up exactly: the seconds of the blocks
// are BlockSeconds, and BlockSeconds + BreakSeconds is the whole seconds of
// the span, no matter how the single values would round on their own.
func TestWorkdayBlockSecondsAddUp(t *testing.T) {
	s, _ := openTestStore(t)
	var sessions []model.Session
	var evs []model.Event
	for i, at := range []string{
		"2026-03-09T08:00:00.900Z", "2026-03-09T08:00:01.800Z", // block of 0.9 s
		"2026-03-09T09:00:00.700Z", "2026-03-09T09:00:02.600Z", // block of 1.9 s
		"2026-03-09T11:00:00.400Z", "2026-03-09T11:00:00.500Z", "2026-03-09T11:00:00.600Z", // 0.2 s
	} {
		id := fmt.Sprintf("claude:f%d", i)
		sessions = append(sessions, model.Session{ID: id, Harness: "claude", NativeID: fmt.Sprintf("f%d", i), Cwd: "/w/app", StartedAt: ts(at), EndedAt: ts(at)})
		evs = append(evs, ev(id+"-1", id, at, model.ActorHuman, model.KindMessage, "user", "x"))
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/f.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
	days, err := s.Workday(ctx, WorkdayFilter{Since: time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC), Until: time.Date(2026, 3, 10, 4, 0, 0, 0, time.UTC),
		Location: time.UTC, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute, HumanOnly: true})
	if err != nil || len(days) != 1 {
		t.Fatalf("%v, %d days", err, len(days))
	}
	d := days[0]
	var got []int
	sum := 0
	for _, b := range d.Blocks {
		got = append(got, b.Secs)
		sum += b.Secs
	}
	span := int(d.Span().Seconds()) // 3 h 0 min 0.5 s... whole seconds
	if len(got) != 3 || sum != d.BlockSeconds() || d.BlockSeconds()+d.BreakSeconds() != span {
		t.Errorf("block seconds %v (sum %d), BlockSeconds %d + BreakSeconds %d, span %d", got, sum, d.BlockSeconds(), d.BreakSeconds(), span)
	}
	if want := []int{0, 2, 0}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("block seconds %v, want %v", got, want)
	}
}
