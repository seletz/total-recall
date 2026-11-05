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
	type slot struct {
		project int
		off     time.Duration
		n       int
	}
	tests := []struct {
		name    string
		keys    int // projects of the window, ranked a, b, c, … by volume
		covered bool
		slots   []slot
		want    string
	}{
		{"not covered", 3, false, nil, "·····████────│··········"},
		{"most events win, tie to the larger project, gap", 3, true,
			[]slot{{0, 5 * h, 1}, {1, 5*h + 30*m, 1}, {0, 6*h + 5*m, 1}, {1, 6*h + 10*m, 2}, {2, 8*h + 59*m, 1}},
			"·····█▓░▒────│··········"},
		{"beyond the palette is the rest", 8, true, []slot{{7, 5 * h, 1}, {6, 6 * h, 1}, {5, 7 * h, 1}, {0, 8 * h, 1}},
			"·····▪▪▌█────│··········"},
		{"events outside the span keep the background", 1, true, []slot{{0, 2 * h, 1}, {0, 5 * h, 1}, {0, 20 * h, 1}},
			"·····█░░░────│··········"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Every project has a day's worth of events in volume order,
			// so the ranks are a, b, c, …; the slots are the cells' events.
			pd := store.ProjectDay{Day: mon, Label: "2026-01-05"}
			for i := range tt.keys {
				pd.Projects = append(pd.Projects, store.DayProject{Key: string(rune('a' + i)), Events: 100 - i})
			}
			for _, s := range tt.slots {
				p := &pd.Projects[s.project]
				p.Slots = append(p.Slots, store.ProjectSlot{Offset: s.off, Events: s.n})
			}
			sh := shown{byProject: true, projects: []store.ProjectDay{pd}, loaded: true}
			if !tt.covered {
				sh.projects = []store.ProjectDay{{Day: mon.AddDate(0, 0, 1), Label: "2026-01-06", Projects: pd.Projects}}
			}
			m := Model{opts: Options{Colors: &chart.Colors{}}, th: theme.New(true), styles: newStyles(theme.New(true)), shown: sh}
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
			t.Errorf("project %d (%s) = %v, want %v", i, p.keys[i], got, want)
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

// TestProjectCache checks how often the query runs with the bars by
// project: the project days come with the days in the one query of a
// load, with the same prefetch, so day steps and the neighbouring periods
// come from the cache; turning the colouring on for a window whose days
// are cached loads once more (the project days are missing), turning it
// off and on again loads nothing, and scrolling never loads.
func TestProjectCache(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tests := []struct {
		name          string
		keys          []string
		loads, pLoads int32
	}{
		{"off loads nothing", []string{"l", "m"}, 2, 0},
		{"on loads the window once more", []string{"p"}, 2, 1},
		{"toggle keeps the cache", []string{"p", "p", "p"}, 2, 1},
		{"on at the start loads once", []string{"P"}, 1, 1},
		{"last week is prefetched", []string{"p", "l", "c", "l"}, 2, 1},
		{"counting mode", []string{"p", "h", "h"}, 3, 2},
		{"scrolling does not load", []string{"p", "m", "l", "j", "j", "k"}, 3, 2},
		{"day steps within the margin", []string{"p", "k", "j", "k"}, 2, 1},
		{"held key fetches once per margin", append([]string{"p"}, repeat("k", 14)...), 3, 2},
		{"a window seen while off loads once on", []string{"l", "p", "c", "l"}, 2, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &countingLoader{st: st}
			m := newModel(t, l)
			if len(tt.keys) > 0 && tt.keys[0] == "P" {
				m = New(Options{Loader: l, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work, Holidays: holidays, HumanOnly: true, Projects: true})
				tt.keys = tt.keys[1:]
			}
			drive(t, m, tt.keys...)
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
	if l.projectCalls.Load() != 1 || l.calls.Load() != 1 {
		t.Errorf("loads = %d, project loads = %d; want 1, 1", l.calls.Load(), l.projectCalls.Load())
	}
}

// TestProjectScrollKeepsContent is TestScrollKeepsContent with the bars by
// project: every frame while the answers are pending shows a complete
// window with its legend, and the frame changes only to a window whose
// days and project days are all at hand. Turning the colouring on or off
// for a window not yet cached that way keeps the frame as well, until the
// answer arrives.
func TestProjectScrollKeepsContent(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	up := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}
	tests := []struct {
		name  string
		setup []string // keys driven to completion first
		msgs  []tea.Msg
		rows  int // day rows on screen
	}{
		{"keys", []string{"p"}, tuitest.Keys(repeat("k", 10)...), 7},
		{"wheel", []string{"p"}, wheel(up, 10), 7},
		{"periods", []string{"p"}, tuitest.Keys("[", "[", "[", "]", "k", "k"), 7},
		{"month", []string{"p", "m"}, wheel(up, 35), 17},
		{"single day", []string{"p", "d"}, tuitest.Keys(repeat("k", 10)...), 1},
		{"on and off while scrolling", []string{"p"}, tuitest.Keys("[", "[", "p", "k", "p", "k", "h", "p"), 7},
		{"turning on", []string{"[", "["}, tuitest.Keys("p"), 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tm tea.Model = drive(t, newModel(t, st), tt.setup...)
			prev := tuitest.Frame(tm)
			var cmds []tea.Cmd
			for i, msg := range tt.msgs {
				var cmd tea.Cmd
				tm, cmd = tm.Update(msg)
				m, f := tm.(Model), tuitest.Frame(tm)
				if strings.Contains(strings.ToLower(f), "loading") {
					t.Errorf("step %d shows a loading state:\n%s", i, f)
				}
				if got := len(dayRow.FindAllString(f, -1)); got != tt.rows {
					t.Errorf("step %d shows %d day rows, want %d:\n%s", i, got, tt.rows, f)
				}
				if m.shown.byProject != strings.Contains(f, "projects: ") {
					t.Errorf("step %d: legend does not match the shown mode:\n%s", i, f)
				}
				if got := strings.Count(f, "\n"); got != bodySize.Height {
					t.Errorf("step %d has %d lines, want %d", i, got, bodySize.Height)
				}
				if cmd != nil && f != prev {
					t.Errorf("step %d started a query and changed the frame:\n%s", i, f)
				}
				prev = f
				cmds = append(cmds, cmd)
			}
			for _, cmd := range cmds {
				tm = tuitest.Run(t, tm, cmd)
			}
			want := tuitest.Drive(t, newModel(t, st), append(append([]tea.Msg{bodySize}, tuitest.Keys(tt.setup...)...), tt.msgs...)...)
			if got, w := tuitest.Frame(tm), tuitest.Frame(want); got != w {
				t.Errorf("after the answers:\n%s\nwant:\n%s", got, w)
			}
			if tm.(Model).pending != nil {
				t.Error("a query is still pending after every answer")
			}
		})
	}
}

// TestProjectStaleStays checks that the legend and the bars keep the data
// on screen while the next window loads, and that a late answer to an
// older window is cached but not shown.
func TestProjectStaleStays(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	// Last week is prefetched; two weeks back are not.
	m := drive(t, newModel(t, st), "p", "[")
	before := tuitest.Frame(m)
	var tm tea.Model
	var cmd tea.Cmd
	tm, cmd = m.Update(tuitest.Key("["))
	m = tm.(Model)
	if cmd == nil || tuitest.Frame(m) != before {
		t.Fatalf("[ did not start a load or changed the frame:\n%s", tuitest.Frame(m))
	}
	stale := cmd()
	cached := len(m.proj.cache)
	// Back to this week (cached), then deliver the stale answer.
	tm, back := m.Update(tuitest.Key("c"))
	tm = tuitest.Run(t, tm, back)
	this := tuitest.Frame(tm)
	tm, _ = tm.Update(stale)
	m = tm.(Model)
	if f := tuitest.Frame(m); f != this || len(m.proj.cache) <= cached {
		t.Errorf("stale answer changed the frame or was not cached (%d cached):\n%s", len(m.proj.cache), f)
	}
}

// TestProjectLoadError: a failing project query is the query's error, so
// the hint and the status line name it.
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
		calls := l.calls.Load()
		tm, cmd := m.Reload()
		m = tuitest.Run(t, tm, cmd).(Model)
		want := int32(0)
		if on {
			want = 2
		}
		if l.projectCalls.Load() != want || l.calls.Load() != calls+1 {
			t.Errorf("on=%v: loads = %d, project loads = %d; want %d, %d", on, l.calls.Load(), l.projectCalls.Load(), calls+1, want)
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
