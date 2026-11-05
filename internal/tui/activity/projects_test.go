package activity

import (
	"bytes"
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestProjectRanks checks which project a cell takes on a 24-cell axis
// (one cell per hour from 04:00): the one with the most counted events in
// the cell, a tie going to the project with more events in the window; a
// cell of the span without events is a gap, a project beyond the palette
// is the folded rest, and a day the data does not cover draws the plain
// bar.
func TestProjectRanks(t *testing.T) {
	loc := testfixture.ActivityLocation
	mon := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	h, m := time.Hour, time.Minute
	day := store.Workday{Day: mon, Label: "2026-01-05", First: mon.Add(9*h + 15*m), Last: mon.Add(13 * h)}
	slot := func(p int, off time.Duration, n int) store.ProjectSlot {
		return store.ProjectSlot{Project: p, Offset: off, Events: n}
	}
	tests := []struct {
		name  string
		keys  int // projects of the window
		slots []store.ProjectSlot
		want  string
	}{
		{"not covered", 3, nil, "·····████────│··········"},
		{"most events win, tie to the larger project, gap",
			3, []store.ProjectSlot{slot(0, 5*h, 1), slot(1, 5*h+30*m, 1), slot(0, 6*h+5*m, 1), slot(1, 6*h+10*m, 2), slot(2, 8*h+59*m, 1)},
			"·····█▓░▒────│··········"},
		{"beyond the palette is the rest", 8, []store.ProjectSlot{slot(7, 5*h, 1), slot(6, 6*h, 1), slot(5, 7*h, 1), slot(0, 8*h, 1)},
			"·····▪▪▌█────│··········"},
		{"events outside the span keep the background", 1, []store.ProjectSlot{slot(0, 2*h, 1), slot(0, 5*h, 1), slot(0, 20*h, 1)},
			"·····█░░░────│··········"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := store.ProjectDays{Keys: make([]string, tt.keys)}
			for i := range data.Keys {
				data.Keys[i] = string(rune('a' + i))
			}
			if tt.slots != nil {
				data.Days = []store.ProjectDay{{Day: mon, Label: "2026-01-05", Slots: tt.slots}}
			}
			m := Model{opts: Options{Colors: &chart.Colors{}}, th: theme.New(true), styles: newStyles(theme.New(true)), proj: projects{on: true, data: data}}
			l := layout{loc: loc, dayStart: 4 * time.Hour, work: work, bar: 24, styles: m.styles, projects: m.projectLayout()}
			cells := l.cells(day)
			if got := stripANSI(l.projects.bar(l, cells, day)); got != tt.want {
				t.Errorf("bar =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestProjectFrames drives the bars by project against the Activity
// fixture and compares the frame with a golden file: the switch on the
// top line, the bars with one glyph per project, the legend below the
// rows with names and counted events, and all of it off again.
func TestProjectFrames(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"ThisWeek", []string{"p"}},
		{"OffAgain", []string{"p", "p"}},
		{"LastWeek", []string{"p", "l"}},
		{"LastWeekAll", []string{"p", "l", "h"}},
		{"LastMonth", []string{"p", "m", "l"}},
		{"LastMonthFiguresOff", []string{"p", "m", "l", "s"}},
		{"HolidayDay", []string{"p", "d", "[", "[", "[", "[", "["}},
		{"NewYearsEve", []string{"d", "l", "p", "[", "[", "[", "[", "[", "[", "[", "[", "[", "["}},
		{"DayBack", []string{"p", "k", "k"}},
		{"UnknownKeyIgnored", []string{"p", "x", "q"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), tt.keys...)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

// demoNow is the clock of the demo frames: the Sunday of the hand-laid
// week of the demo archive, as in the screenshots.
var demoNow = time.Date(2026, 6, 7, 12, 0, 0, 0, demo.Location)

// TestProjectDemoFrames renders the hand-laid week of the demo archive by
// project: several projects per day, the folded rest, and the same with
// all events, where the bars have fewer gaps.
func TestProjectDemoFrames(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"Week", []string{"p"}},
		{"WeekAll", []string{"p", "h"}},
		{"Month", []string{"p", "m"}},
		{"Day", []string{"p", "d", "k", "k", "k", "k", "k"}},
	}
	st, _ := fixturedb.Demo(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Options{Loader: st, Now: func() time.Time { return demoNow }, Location: demo.Location, DayStart: 4 * time.Hour, Work: work, Holidays: holidays, HumanOnly: true, Home: demo.Home})
			m = drive(t, m, tt.keys...)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

// TestProjectColors pins the escape sequences of the bars by project on a
// dark and a light background: the project colours of the shared
// chart.Colors, toned for the background, the muted rest and the gaps.
func TestProjectColors(t *testing.T) {
	tests := []struct {
		name string
		bg   color.Color
		keys []string
	}{
		{"LastMonthDark", color.Black, []string{"p", "m", "l"}},
		{"LastMonthLight", color.White, []string{"p", "m", "l"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []tea.Msg{tea.WindowSizeMsg{Width: 110, Height: 40}, tea.BackgroundColorMsg{Color: tt.bg}}
			msgs = append(msgs, tuitest.Keys(tt.keys...)...)
			m := tuitest.Drive(t, newModel(t, st), msgs...).(Model)
			golden.RequireEqual(t, []byte(m.View().Content))
		})
	}
}

// TestProjectSharedColors checks that the colours come from the shared
// chart.Colors: a project that received its colour elsewhere (the stats
// view) keeps it here, whatever its rank in the window.
func TestProjectSharedColors(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	colors := &chart.Colors{}
	// Colours assigned before the view is used, in reverse volume order.
	colors.Color(testfixture.ActivityGamma)
	colors.Color(testfixture.ActivityBeta)
	colors.Color(testfixture.ActivityAlpha)
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work, Colors: colors})
	m = drive(t, m, "p", "m", "l")
	p := m.projectLayout()
	if len(p.styles) != 3 {
		t.Fatalf("%d projects, want 3", len(p.styles))
	}
	// Alpha leads last month but took the third colour.
	for i, want := range []color.Color{chart.Palette[2], chart.Palette[1], chart.Palette[0]} {
		if got := p.styles[i].GetForeground(); !sameColor(got, want) {
			t.Errorf("project %d (%s) = %v, want %v", i, m.proj.data.Keys[i], got, want)
		}
	}
}

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// TestProjectNoColor writes a frame by project through the colour profile
// NO_COLOR selects: no colours remain, while the bars and the legend keep
// one glyph per project, so the projects are still told apart.
func TestProjectNoColor(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := tuitest.Drive(t, newModel(t, st), bodySize, tea.BackgroundColorMsg{Color: color.Black}, tuitest.Key("p"), tuitest.Key("m"), tuitest.Key("l")).(Model)
	profile := colorprofile.Env([]string{"TERM=xterm-256color", "NO_COLOR=1"})
	var buf bytes.Buffer
	w := colorprofile.Writer{Forward: &buf, Profile: profile}
	if _, err := w.Write([]byte(m.View().Content)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, bad := range []string{"38;2;", "48;2;", "38;5;", "48;5;"} {
		if strings.Contains(out, bad) {
			t.Errorf("colourless output carries %q", bad)
		}
	}
	plain := stripANSI(out)
	for _, want := range []string{"bars: hours [projects]", "projects: █ alpha", "▓ beta", "▒ /w/gamma"} {
		if !strings.Contains(plain, want) {
			t.Errorf("colourless output lacks %q:\n%s", want, plain)
		}
	}
}

// TestProjectCache checks that the project data is loaded once per window
// and counting mode, only while the bars are by project, and never by
// scrolling or toggling back to a seen window; the days are not loaded
// again by the toggle.
func TestProjectCache(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tests := []struct {
		name          string
		keys          []string
		loads, pLoads int32
	}{
		{"off loads nothing", []string{"l", "m"}, 3, 0},
		{"on loads once", []string{"p"}, 1, 1},
		{"toggle keeps the cache", []string{"p", "p", "p"}, 1, 1},
		{"toggle does not reload the days", []string{"l", "p"}, 2, 1},
		{"window back and forth", []string{"p", "l", "c", "l"}, 2, 2},
		{"counting mode", []string{"p", "h", "h"}, 2, 2},
		{"scrolling does not load", []string{"p", "m", "l", "j", "j", "k"}, 3, 3},
		{"day step loads once per window", []string{"p", "k", "j", "k"}, 2, 2},
		{"a window seen while off loads once on", []string{"l", "p", "c", "l"}, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &countingLoader{st: st}
			drive(t, newModel(t, l), tt.keys...)
			if got, pgot := l.calls.Load(), l.projectCalls.Load(); got != tt.loads || pgot != tt.pLoads {
				t.Errorf("loads = %d, project loads = %d; want %d, %d", got, pgot, tt.loads, tt.pLoads)
			}
		})
	}
}

// TestProjectStart: the view starts with the bars by project when the
// options say so, and loads the project data with the days.
func TestProjectStart(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	l := &countingLoader{st: st}
	m := New(Options{Loader: l, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work, Holidays: holidays, HumanOnly: true, Projects: true})
	m = drive(t, m)
	f := tuitest.Frame(m)
	if !m.proj.on || !strings.Contains(f, "bars: hours [projects]") || !strings.Contains(f, "projects: █") {
		t.Errorf("bars are not by project at start:\n%s", f)
	}
	if l.projectCalls.Load() != 1 {
		t.Errorf("project loads = %d, want 1", l.projectCalls.Load())
	}
}

// TestProjectStaleStays checks that the legend and the bars keep the data
// on screen while the next window loads, and that a late answer to an
// older window is cached but not shown.
func TestProjectStaleStays(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := drive(t, newModel(t, st), "p")
	before := m.legend()
	// Start the loads for last week and run them, but do not deliver the
	// answers yet.
	var tm tea.Model
	var cmd tea.Cmd
	tm, cmd = m.Update(tuitest.Key("l"))
	m = tm.(Model)
	if m.legend() != before {
		t.Errorf("legend changed before the answer:\n%s\n%s", stripANSI(before), stripANSI(m.legend()))
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("l did not start two loads: %T", cmd())
	}
	var stale []tea.Msg
	for _, c := range batch {
		stale = append(stale, c())
	}
	// Back to this week (cached), then deliver the stale answers.
	tm, back := m.Update(tuitest.Key("c"))
	tm = tuitest.Run(t, tm, back)
	for _, msg := range stale {
		tm, _ = tm.Update(msg)
	}
	m = tm.(Model)
	if m.legend() != before || len(m.proj.cache) != 2 {
		t.Errorf("stale answer changed the legend or was not cached (%d cached):\n%s", len(m.proj.cache), stripANSI(m.legend()))
	}
}

// TestProjectLoadError: a failing project query names its error in the
// legend; the days are still shown.
func TestProjectLoadError(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	l := &countingLoader{st: st, projectErr: errors.New("boom")}
	m := drive(t, newModel(t, l), "p")
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestProjectReload: Reload loads the project data again while the bars
// are by project, keeps the frame and does nothing for them while off.
func TestProjectReload(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	for _, on := range []bool{true, false} {
		l := &countingLoader{st: st}
		var keys []string
		if on {
			keys = []string{"p"}
		}
		m := drive(t, newModel(t, l), keys...)
		before := tuitest.Frame(m)
		tm, cmd := m.Reload()
		m = tuitest.Run(t, tm, cmd).(Model)
		want := int32(0)
		if on {
			want = 2
		}
		if l.projectCalls.Load() != want || l.calls.Load() != 2 {
			t.Errorf("on=%v: loads = %d, project loads = %d; want 2, %d", on, l.calls.Load(), l.projectCalls.Load(), want)
		}
		if got := tuitest.Frame(m); got != before {
			t.Errorf("on=%v: reload changed the frame:\n%s\n%s", on, before, got)
		}
	}
}

// TestProjectClick checks the switch on the top line: a click on
// "projects" turns the bars by project on, a click on "hours" off, and the
// active one is a no-op.
func TestProjectClick(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	click := func(x int) tea.MouseClickMsg { return tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft} }
	tests := []struct {
		name   string
		keys   []string
		label  string
		on     bool
		pLoads int32
	}{
		{"projects on", nil, "projects", true, 1},
		{"hours off", []string{"p"}, "hours", false, 1},
		{"active projects is a no-op", []string{"p"}, "projects", true, 1},
		{"active hours is a no-op", nil, "hours", false, 0},
		{"label is inert", nil, "bars:", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &countingLoader{st: st}
			m := drive(t, newModel(t, l), tt.keys...)
			m = tuitest.Send(t, m, click(switchX(t, m, tt.label))).(Model)
			if m.proj.on != tt.on || l.projectCalls.Load() != tt.pLoads {
				t.Errorf("on = %v, project loads = %d; want %v, %d", m.proj.on, l.projectCalls.Load(), tt.on, tt.pLoads)
			}
		})
	}
}

// TestProjectLegendRows: the legend takes a row from the day rows, so a
// month scrolls one row earlier, and the figures give way one row earlier.
func TestProjectLegendRows(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := drive(t, newModel(t, st), "m", "l")
	off := m.rowsAvail()
	m = drive(t, m, "p")
	if on := m.rowsAvail(); on != off-1 {
		t.Errorf("rows with the legend = %d, want %d", on, off-1)
	}
	if !strings.Contains(tuitest.Frame(m), "projects: ") {
		t.Error("frame lacks the legend")
	}
}
