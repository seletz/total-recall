package dayview

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/dayreport"
	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
	"github.com/seletz/total-recall/internal/worktime"
)

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, demo.Location)

func reportOptions(human bool) dayreport.Options {
	return dayreport.Options{
		Location: demo.Location, DayStart: 4 * time.Hour, BlockGap: 30 * time.Minute, HumanOnly: human,
		Work:     store.WorkHours{Start: 9 * time.Hour, End: 17 * time.Hour, Target: 8 * time.Hour, Low: 6 * time.Hour, High: 9 * time.Hour, WeeklyLow: 32 * time.Hour, WeeklyHigh: 48 * time.Hour},
		Holidays: holiday.New("Baden-Württemberg"),
		Worktime: worktime.Settings{MaxGap: 20 * time.Minute, MinCredit: 2 * time.Minute, Grid: 15 * time.Minute},
	}
}

func view(t *testing.T, st *store.Store, w, h int, human bool) Model {
	t.Helper()
	m := New(Options{Source: st, Report: reportOptions(human), Now: func() time.Time { return now }})
	return tuitest.Send(t, m, tea.WindowSizeMsg{Width: w, Height: h}).(Model)
}

func open(t *testing.T, m Model, day string, human bool) Model {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", day, demo.Location)
	if err != nil {
		t.Fatal(err)
	}
	return tuitest.Send(t, m, Open{Day: d, HumanOnly: human}).(Model)
}

func plain(m Model) string { return ansi.Strip(m.View().Content) }

func activeDays(t *testing.T, st *store.Store, human bool) []store.Workday {
	t.Helper()
	o := reportOptions(human)
	days, err := st.Workday(context.Background(), store.WorkdayFilter{Location: o.Location, DayStart: o.DayStart, HumanOnly: human, Holidays: o.Holidays, BlockGap: o.BlockGap})
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Workday
	for _, d := range days {
		if d.Active() {
			out = append(out, d)
		}
	}
	return out
}

// TestShowsTheNumbersOfStats is the acceptance invariant on the rendered
// view: for every active day of the demo archive, in both counting modes,
// the head shows the numbers of stats --workday --blocks, every block its
// seconds, events and projects, and the project rows the time of
// stats --worktime and the events by human and agent.
func TestShowsTheNumbersOfStats(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	for _, human := range []bool{true, false} {
		m := view(t, st, 140, 80, human)
		for _, d := range activeDays(t, st, human) {
			frame := plain(open(t, m, d.Label, human))
			loc := demo.Location
			want := []string{
				d.FirstClock(loc) + "–" + d.LastClock(loc), "span " + store.FormatSpan(d.Span()),
				"work " + store.FormatSpan(d.BlockSum()), "breaks " + store.FormatSpan(d.BreakSum()),
			}
			for _, b := range d.Blocks {
				span := d.Clock(b.Start, loc) + "–" + d.Clock(b.End, loc)
				if b.Single() {
					span = d.Clock(b.Start, loc) + "*"
				}
				want = append(want, span, store.FormatSpan(time.Duration(b.Secs)*time.Second))
			}
			rep, err := dayreport.Load(context.Background(), st, d.Day, reportOptions(human))
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range rep.Projects {
				ok := false
				for _, line := range strings.Split(frame, "\n") {
					f := strings.Fields(line)
					if len(f) >= 5 && f[len(f)-5] == p.Name && f[len(f)-4] == store.FormatSpan(p.Raw) && f[len(f)-3] == store.FormatSpan(p.Rounded) &&
						f[len(f)-2] == fmt.Sprint(p.Human) && f[len(f)-1] == fmt.Sprint(p.Agent) {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s (human=%v): no row for project %+v:\n%s", d.Label, human, p, frame)
				}
			}
			// The categories add up to the day, raw and rounded, as --worktime.
			craw, crounded := time.Duration(0), time.Duration(0)
			for _, c := range rep.Worktime.Categories {
				craw += c.Raw
				crounded += c.Rounded
				ok := false
				for _, line := range strings.Split(frame, "\n") {
					f := strings.Fields(line)
					if len(f) == 3 && f[0] == c.Name && f[1] == store.FormatSpan(c.Raw) && f[2] == store.FormatSpan(c.Rounded) {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s (human=%v): no row for category %+v:\n%s", d.Label, human, c, frame)
				}
			}
			if craw != rep.Worktime.Raw || crounded != rep.Worktime.Rounded || !strings.Contains(frame, "categories") {
				t.Errorf("%s: categories %s/%s, day %s/%s", d.Label, craw, crounded, rep.Worktime.Raw, rep.Worktime.Rounded)
			}
			for _, w := range want {
				if !strings.Contains(frame, w) {
					t.Errorf("%s (human=%v): view lacks %q:\n%s", d.Label, human, w, frame)
					break
				}
			}
		}
	}
}

func TestNavigation(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	m := open(t, view(t, st, 120, 36, true), "2026-10-29", true)
	if m.Day().Format("2006-01-02") != "2026-10-29" {
		t.Fatal("not opened on the day")
	}
	for _, tc := range []struct {
		key  string
		want string
	}{{"]", "2026-10-30"}, {"left", "2026-10-29"}, {"[", "2026-10-28"}, {"right", "2026-10-29"}} {
		m = tuitest.Send(t, m, tuitest.Key(tc.key)).(Model)
		if got := m.Day().Format("2006-01-02"); got != tc.want {
			t.Fatalf("after %s: day %s, want %s", tc.key, got, tc.want)
		}
		if !strings.Contains(plain(m), tc.want) {
			t.Errorf("title does not show %s", tc.want)
		}
	}
	// not past today
	m = open(t, m, "2026-11-01", true)
	m = tuitest.Send(t, m, tuitest.Key("]")).(Model)
	if m.Day().Format("2006-01-02") != "2026-11-01" {
		t.Errorf("moved past today to %s", m.Day().Format("2006-01-02"))
	}
}

func TestBackAndOpenSession(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	m := open(t, view(t, st, 120, 36, true), "2026-10-29", true)
	_, cmd := m.Update(tuitest.Key("esc"))
	if b, ok := cmd().(Back); !ok || b.Day.Format("2006-01-02") != "2026-10-29" {
		t.Fatalf("esc: %#v", cmd())
	}
	rep, err := dayreport.Load(context.Background(), st, m.Day(), reportOptions(true))
	if err != nil {
		t.Fatal(err)
	}
	// enter on a block with several sessions lists them, a second enter (the
	// block is expanded) opens the first at its first event
	first := rep.Blocks[0].Sessions[0]
	if len(rep.Blocks[0].Sessions) < 2 {
		t.Fatal("the first block of the day has one session")
	}
	m2, cmd := m.Update(tuitest.Key("enter"))
	if cmd != nil || len(m2.(Model).items()) != len(m.items())+len(rep.Blocks[0].Sessions) {
		t.Fatalf("enter on a collapsed block: cmd %v, %d items", cmd, len(m2.(Model).items()))
	}
	m = m2.(Model)
	_, cmd = m.Update(tuitest.Key("enter"))
	if o, ok := cmd().(OpenSession); !ok || o.SessionID != first.SessionID || o.EventID != first.FirstEvent {
		t.Fatalf("enter on the expanded block: %#v, want %+v", cmd(), first)
	}
	// j moves to the first session row; enter opens that one
	m = tuitest.Send(t, m, tuitest.Key("j")).(Model)
	_, cmd = m.Update(tuitest.Key("enter"))
	if o, ok := cmd().(OpenSession); !ok || o.SessionID != first.SessionID {
		t.Fatalf("enter on session: %#v", cmd())
	}
	// a click on a row opens it, a click on the arrows moves the day
	l := m.layout()
	for y, r := range l.rows {
		if r.item == m.cursor {
			_, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: l.bodyTop + y})
			if o, ok := cmd().(OpenSession); !ok || o.SessionID != first.SessionID {
				t.Fatalf("click on session: %#v", cmd())
			}
		}
	}
	_, prev, _ := m.title()
	m3 := tuitest.Send(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: prev.Start, Y: 0}).(Model)
	if m3.Day().Format("2006-01-02") != "2026-10-28" {
		t.Errorf("click on the arrow: %s", m3.Day().Format("2006-01-02"))
	}
}

// TestExpandSessions: the sessions of a block are hidden until the block is
// expanded with space or enter; the cursor stays on the block.
func TestExpandSessions(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	rep, err := dayreport.Load(context.Background(), st, time.Date(2026, 10, 26, 0, 0, 0, 0, demo.Location), reportOptions(true))
	if err != nil {
		t.Fatal(err)
	}
	multi := -1
	for i, b := range rep.Blocks {
		if len(b.Sessions) > 1 {
			multi = i
			break
		}
	}
	if multi < 0 {
		t.Fatal("no block with several sessions")
	}
	m := open(t, view(t, st, 120, 40, true), "2026-10-26", true)
	for i := 0; i < multi; i++ {
		m = tuitest.Send(t, m, tuitest.Key("j")).(Model)
	}
	title := rep.Blocks[multi].Sessions[1].Title
	if title == "" || strings.Contains(plain(m), title) {
		t.Fatalf("collapsed view shows the session %q (or it has no title)", title)
	}
	n := len(m.items())
	for _, k := range []string{"space", "enter"} {
		m = tuitest.Send(t, m, tuitest.Key(k)).(Model)
		if got := len(m.items()); got != n+len(rep.Blocks[multi].Sessions) || !strings.Contains(plain(m), title) {
			t.Fatalf("%s did not list the sessions: %d items (want %d), title %q:\n%s", k, got, n+len(rep.Blocks[multi].Sessions), title, plain(m))
		}
		if it := m.items()[m.cursor]; it.block != multi || it.session != -1 {
			t.Fatalf("%s moved the cursor to %+v", k, it)
		}
		// hide again with space (the second round expands with enter)
		if k == "space" {
			m = tuitest.Send(t, m, tuitest.Key("space")).(Model)
			if len(m.items()) != n || strings.Contains(plain(m), title) {
				t.Fatal("space did not hide the sessions")
			}
		}
	}
	// on a session row space hides the block's sessions and selects the block
	m = tuitest.Send(t, m, tuitest.Key("j")).(Model)
	m = tuitest.Send(t, m, tuitest.Key("space")).(Model)
	if it := m.items()[m.cursor]; len(m.items()) != n || it.block != multi || it.session != -1 {
		t.Fatalf("space on a session: %d items, cursor %+v", len(m.items()), it)
	}
}

func TestDayWithoutActivity(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	m := open(t, view(t, st, 120, 36, true), "2020-01-01", true)
	if !strings.Contains(plain(m), "no activity on this day") {
		t.Errorf("empty day:\n%s", plain(m))
	}
	// keys do nothing harmful on an empty day
	m = tuitest.Drive(t, m, tuitest.Key("j"), tuitest.Key("enter")).(Model)
	if !strings.Contains(plain(m), "no activity") {
		t.Error("keys changed the empty day")
	}
}

func TestHolidayAndWeekend(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	// Fronleichnam 2026-06-04 is in the demo archive's hand-laid week
	if f := plain(open(t, view(t, st, 120, 36, true), "2026-06-04", true)); !strings.Contains(f, "Fronleichnam") {
		t.Errorf("holiday not named:\n%s", f)
	}
	if f := plain(open(t, view(t, st, 120, 36, true), "2026-06-06", true)); !strings.Contains(f, "weekend") {
		t.Errorf("weekend not marked:\n%s", f)
	}
}

// TestPastMidnight: a day whose last activity is after midnight shows the
// clock time with the number of days after, as stats does.
func TestPastMidnight(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	found := false
	for _, d := range activeDays(t, st, false) {
		if !strings.Contains(d.LastClock(demo.Location), "(+1)") {
			continue
		}
		found = true
		if f := plain(open(t, view(t, st, 140, 80, false), d.Label, false)); !strings.Contains(f, d.LastClock(demo.Location)) {
			t.Errorf("%s: no %q:\n%s", d.Label, d.LastClock(demo.Location), f)
		}
	}
	if !found {
		t.Skip("the demo archive has no day past midnight")
	}
}

func TestNarrowAndColourless(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	for _, w := range []int{40, 60, 80, 120} {
		m := open(t, view(t, st, w, 24, true), "2026-10-29", true)
		for i, line := range strings.Split(plain(m), "\n") {
			if n := len([]rune(line)); n > w {
				t.Errorf("width %d: line %d has %d cells: %q", w, i, n, line)
			}
		}
	}
	// without colours the projects still differ by glyph
	f := plain(open(t, view(t, st, 120, 36, false), "2026-10-29", false))
	if !strings.Contains(f, "█") {
		t.Errorf("no project glyph:\n%s", f)
	}
}

func TestHumanToggleReloads(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	m := open(t, view(t, st, 120, 36, true), "2026-10-29", true)
	if !strings.Contains(plain(m), "human events only") {
		t.Fatal("not human only")
	}
	m = tuitest.Send(t, m, tuitest.Key("h")).(Model)
	if !strings.Contains(plain(m), "all events") {
		t.Errorf("toggle did not switch:\n%s", plain(m))
	}
}

func TestNoDatabase(t *testing.T) {
	m := New(Options{Report: reportOptions(true)})
	m = tuitest.Send(t, m, tea.WindowSizeMsg{Width: 80, Height: 20}).(Model)
	m = tuitest.Send(t, m, Open{Day: now}).(Model)
	if !strings.Contains(plain(m), "no database") {
		t.Errorf("no hint:\n%s", plain(m))
	}
}

// TestScrollsToTheProjects: with a body too short for everything, moving
// past the last row scrolls on to the projects of the day.
func TestScrollsToTheProjects(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	m := open(t, view(t, st, 100, 16, true), "2026-06-02", true)
	if strings.Contains(plain(m), "total") {
		t.Fatal("the projects fit already; the test needs a shorter body")
	}
	for range 40 {
		m = tuitest.Send(t, m, tuitest.Key("j")).(Model)
	}
	if f := plain(m); !strings.Contains(f, "total") || !strings.Contains(f, "fahrtenbuch") {
		t.Errorf("projects not reached:\n%s", f)
	}
	for range 40 {
		m = tuitest.Send(t, m, tuitest.Key("k")).(Model)
	}
	if f := plain(m); !strings.Contains(f, "▸") || !strings.Contains(f, "09:10") {
		t.Errorf("did not scroll back:\n%s", f)
	}
}
