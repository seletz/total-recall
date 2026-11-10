package dayreport_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/dayreport"
	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/worktime"
)

func TestMain(m *testing.M) { os.Exit(fixturedb.DemoMain(m)) }

func opts(humanOnly bool) dayreport.Options {
	return dayreport.Options{
		Location: demo.Location, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute, HumanOnly: humanOnly,
		Work:     store.WorkHours{Start: 9 * time.Hour, End: 17 * time.Hour, Target: 8 * time.Hour, Low: 6 * time.Hour, High: 9 * time.Hour, WeeklyLow: 32 * time.Hour, WeeklyHigh: 48 * time.Hour},
		Holidays: holiday.New("Baden-Württemberg"),
		Worktime: worktime.Settings{MaxGap: 20 * time.Minute, MinCredit: 2 * time.Minute, Grid: 15 * time.Minute},
		Projects: project.Resolver{},
	}
}

// TestInvariants checks, for every day of the demo archive and both
// counting modes, that the report adds up and equals what the stats
// functions give for the same day.
func TestInvariants(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	ctx := context.Background()
	for _, human := range []bool{true, false} {
		o := opts(human)
		all, err := st.Workday(ctx, store.WorkdayFilter{Location: o.Location, DayStart: o.DayStart, HumanOnly: human, Holidays: o.Holidays, BlockGap: o.BlockGap})
		if err != nil {
			t.Fatal(err)
		}
		active := 0
		for _, want := range all {
			if !want.Active() {
				continue
			}
			active++
			r, err := dayreport.Load(ctx, st, want.Day, o)
			if err != nil {
				t.Fatalf("%s: %v", want.Label, err)
			}
			name := want.Label
			if r.Workday.Span() != want.Span() || r.Workday.BlockSeconds() != want.BlockSeconds() || r.Workday.BreakSeconds() != want.BreakSeconds() {
				t.Errorf("%s: header differs from stats --workday", name)
			}
			if got := r.Workday.BlockSeconds() + r.Workday.BreakSeconds(); got != int(want.Span().Seconds()) {
				t.Errorf("%s: blocks+breaks %d != span %d", name, got, int(want.Span().Seconds()))
			}
			if len(r.Blocks) != len(want.Blocks) {
				t.Fatalf("%s: %d blocks, want %d", name, len(r.Blocks), len(want.Blocks))
			}
			var ev, hu, ag int
			for i, b := range r.Blocks {
				if b.Start != want.Blocks[i].Start || b.End != want.Blocks[i].End || b.Events != want.Blocks[i].Events || b.Secs != want.Blocks[i].Secs {
					t.Errorf("%s block %d differs", name, i)
				}
				ps, pe := 0, 0
				for _, p := range b.Projects {
					ps += p.Secs
					pe += p.Human + p.Agent
				}
				if ps != b.Secs {
					t.Errorf("%s block %d: project secs %d != %d", name, i, ps, b.Secs)
				}
				if pe != b.Events || b.Human+b.Agent != b.Events {
					t.Errorf("%s block %d: events do not add up", name, i)
				}
				ev += b.Events
				hu += b.Human
				ag += b.Agent
			}
			if ev != want.Human+want.Agent || hu != want.Human || ag != want.Agent {
				t.Errorf("%s: events %d/%d/%d, want %d/%d", name, ev, hu, ag, want.Human, want.Agent)
			}
			var raw, rounded time.Duration
			ph, pa := 0, 0
			for _, p := range r.Projects {
				raw += p.Raw
				rounded += p.Rounded
				ph += p.Human
				pa += p.Agent
			}
			if ph != want.Human || pa != want.Agent {
				t.Errorf("%s: project events %d/%d, want %d/%d", name, ph, pa, want.Human, want.Agent)
			}
			wt, err := worktimeDay(ctx, st, o, want.Day)
			if err != nil {
				t.Fatal(err)
			}
			if r.Worktime.Raw != wt.Raw || raw != wt.Raw {
				t.Errorf("%s: worktime raw %s / project sum %s, want %s", name, r.Worktime.Raw, raw, wt.Raw)
			}
			if r.Worktime.Rounded != wt.Rounded {
				t.Errorf("%s: worktime rounded %s, want %s", name, r.Worktime.Rounded, wt.Rounded)
			}
			if d, ok := o.Work.Diff(want); ok != r.HasDiff || d != r.Diff {
				t.Errorf("%s: diff %s, want %s", name, r.Diff, d)
			}
		}
		if active < 5 {
			t.Fatalf("demo archive gave only %d active days", active)
		}
	}
}

func TestInactiveDay(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	r, err := dayreport.Load(context.Background(), st, time.Date(2020, 1, 1, 0, 0, 0, 0, demo.Location), opts(true))
	if err != nil {
		t.Fatal(err)
	}
	if r.Workday.Active() || len(r.Blocks) != 0 || len(r.Projects) != 0 {
		t.Errorf("inactive day not empty: %+v", r)
	}
}

func worktimeDay(ctx context.Context, st *store.Store, o dayreport.Options, day time.Time) (worktime.Day, error) {
	since, until := store.DayBoundary(day, o.DayStart), store.DayBoundary(day.AddDate(0, 0, 1), o.DayStart)
	evs, err := st.HumanEvents(ctx, store.HumanEventsFilter{Since: since, Until: until})
	if err != nil {
		return worktime.Day{}, err
	}
	in := make([]worktime.Event, len(evs))
	for i, e := range evs {
		in[i] = worktime.Event{Time: e.Time, Session: e.SessionID, Title: e.Title, Project: e.Project, Dir: e.Dir, Remote: e.RemoteURL, Text: e.Text}
	}
	rep := worktime.Compute(in, o.Worktime, worktime.Options{Location: o.Location, DayStart: o.DayStart, Projects: o.Projects, From: day, To: day})
	return rep.Days[0], nil
}
