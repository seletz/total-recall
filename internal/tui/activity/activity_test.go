package activity

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// now is the clock of the view tests: a Sunday noon at the end of the
// Activity fixture, so this week is 2026-W02 and last week 2026-W01 with
// the session that crosses midnight on New Year's Eve.
var now = time.Date(2026, 1, 11, 12, 0, 0, 0, testfixture.ActivityLocation)

func clock() time.Time { return now }

// bodySize is the window size the app shell hands the view at 100×30.
var bodySize = tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height - 4}

// work is the built-in working time: 09:00–17:00, 8h, 6h–9h on target.
var work = store.WorkHours{Start: 9 * time.Hour, End: 17 * time.Hour, Target: 8 * time.Hour, Low: 6 * time.Hour, High: 9 * time.Hour}

func newModel(t *testing.T, l Loader) Model {
	t.Helper()
	return New(Options{Loader: l, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work})
}

// drive starts m with the body size and applies the key names in order,
// running every command to completion (see tuitest.Drive).
func drive(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	msgs := append([]tea.Msg{bodySize}, tuitest.Keys(keys...)...)
	return tuitest.Drive(t, m, msgs...).(Model)
}

// TestFrames drives the view against the Activity fixture and compares the
// frame with a golden file.
func TestFrames(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"Start", nil},
		{"Day", []string{"d"}},
		{"Month", []string{"m"}},
		{"LastWeek", []string{"l"}},
		{"LastWeekHuman", []string{"l", "h"}},
		{"LastMonth", []string{"m", "l"}},
		{"Yesterday", []string{"d", "l"}},
		{"ShiftBack", []string{"["}},
		{"ShiftBackTwice", []string{"[", "["}},
		{"ShiftBackForth", []string{"[", "]"}},
		{"ShiftForwardEmpty", []string{"]"}},
		{"CurrentAfterShift", []string{"[", "[", "c"}},
		{"HumanOff", []string{"h", "h"}},
		{"DayKeepsAnchor", []string{"[", "d"}},
		{"MonthScroll", []string{"m", "l", "j", "j", "j"}},
		{"MonthScrollKeys", []string{"m", "l", "down", "down", "up"}},
		{"MonthScrollEnd", []string{"m", "l", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j"}},
		{"ScrollResetsOnWindow", []string{"m", "l", "j", "j", "]"}},
		{"UnknownKeyIgnored", []string{"x", "q", "1", "e"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), tt.keys...)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

// TestSizes checks the layout at other sizes: the bar goes when there is no
// room for it, the month scrolls in a low body, and every frame has exactly
// the body height.
func TestSizes(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		keys          []string
	}{
		{"Narrow", 80, 20, nil},
		{"NarrowMonth", 80, 20, []string{"m", "l"}},
		{"NoBar", 50, 20, nil},
		{"Tiny", 40, 8, []string{"m", "l"}},
		{"Wide", 140, 40, []string{"m", "l"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), tt.keys...)
			var tm tea.Model
			tm, _ = m.Update(tea.WindowSizeMsg{Width: tt.width, Height: tt.height})
			f := tuitest.Frame(tm)
			if got := strings.Count(f, "\n"); got != tt.height {
				t.Errorf("frame has %d lines, want %d:\n%s", got, tt.height, f)
			}
			golden.RequireEqual(t, f)
		})
	}
}

// TestMidnightDayStart renders last week with the day beginning at
// midnight: the New Year's Eve session is split over two days.
func TestMidnightDayStart(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, Work: work})
	m = drive(t, m, "l")
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestColors pins the escape sequences of the coloured frames: the bar
// segments, the band and its lines, the ratings of span and difference,
// the weekend rows and the total line, on a dark and on a light background
// (the terminal's answer arrives as tea.BackgroundColorMsg). Last month
// holds every edge case of the fixture; the sizes are large enough for all
// of its rows.
func TestColors(t *testing.T) {
	tests := []struct {
		name string
		bg   color.Color
		size tea.WindowSizeMsg
		keys []string
	}{
		{"LastMonthDark", color.Black, tea.WindowSizeMsg{Width: 110, Height: 40}, []string{"m", "l"}},
		{"LastMonthLight", color.White, tea.WindowSizeMsg{Width: 110, Height: 40}, []string{"m", "l"}},
		{"LastWeekDark", color.Black, bodySize, []string{"l"}},
		{"ThisWeekDefaultIsDark", nil, bodySize, nil},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []tea.Msg{tt.size}
			if tt.bg != nil {
				msgs = append(msgs, tea.BackgroundColorMsg{Color: tt.bg})
			}
			msgs = append(msgs, tuitest.Keys(tt.keys...)...)
			m := tuitest.Drive(t, newModel(t, st), msgs...).(Model)
			golden.RequireEqual(t, []byte(m.View().Content))
		})
	}
}

// TestNoWorkHours renders last month without working time: no difference
// column, no band, plain bars, the total without a balance.
func TestNoWorkHours(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour})
	m = drive(t, m, "m", "l")
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestProgram runs the view in a test program: the first load arrives
// asynchronously.
func TestProgram(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tm := tuitest.New(t, newModel(t, st))
	tuitest.WaitFor(t, tm, "total:")
	final := tuitest.Final(t, tm).(Model)
	golden.RequireEqual(t, tuitest.Frame(final))
}

// TestNoStore renders the hint shown without a database; keys do nothing.
func TestNoStore(t *testing.T) {
	tm := tuitest.New(t, newModel(t, nil))
	tuitest.WaitFor(t, tm, "no database")
	tuitest.Press(tm, "m", "l", "h", "[")
	final := tuitest.Final(t, tm).(Model)
	golden.RequireEqual(t, tuitest.Frame(final))
}

// countingLoader counts calls and can fail.
type countingLoader struct {
	st    *store.Store
	calls atomic.Int32
	err   error
}

func (l *countingLoader) Workday(ctx context.Context, f store.WorkdayFilter) ([]store.Workday, error) {
	l.calls.Add(1)
	if l.err != nil {
		return nil, l.err
	}
	return l.st.Workday(ctx, f)
}

func TestLoadError(t *testing.T) {
	l := &countingLoader{err: errors.New("boom")}
	m := drive(t, newModel(t, l))
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestCache checks that a filter combination is loaded once: switching
// back to a seen window or counting mode shows the cached days without a
// command; scrolling never loads.
func TestCache(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tests := []struct {
		name  string
		keys  []string
		loads int32
	}{
		{"start", nil, 1},
		{"same period again", []string{"w"}, 1},
		{"current again", []string{"c"}, 1},
		{"period back and forth", []string{"d", "w"}, 2},
		{"last and current", []string{"l", "c", "l"}, 2},
		{"shift back and forth", []string{"[", "]"}, 2},
		{"shift equals last", []string{"[", "l"}, 2},
		{"human back and forth", []string{"h", "h", "h"}, 2},
		{"scrolling does not load", []string{"m", "j", "j", "k"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &countingLoader{st: st}
			drive(t, newModel(t, l), tt.keys...)
			if got := l.calls.Load(); got != tt.loads {
				t.Errorf("loads = %d, want %d", got, tt.loads)
			}
		})
	}
}

// TestStaleResultDropped checks that the answer to an older load does not
// replace the current data, but is cached.
func TestStaleResultDropped(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := drive(t, newModel(t, st))
	// Start a load for last week but do not run its command; then switch
	// back to this week (cached) and deliver the stale answer.
	var tm tea.Model
	var cmd tea.Cmd
	tm, cmd = m.Update(tuitest.Key("l"))
	stale := cmd()
	tm, cmd = tm.Update(tuitest.Key("c"))
	tm = tuitest.Run(t, tm, cmd)
	tm, _ = tm.Update(stale)
	m = tm.(Model)
	if !strings.Contains(tuitest.Frame(m), "this week") || len(m.cache) != 2 {
		t.Errorf("stale result changed the view or was not cached:\n%s", tuitest.Frame(m))
	}
}

// TestWheel checks that the mouse wheel scrolls the day rows like ↑/↓.
func TestWheel(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	down := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}
	tests := []struct {
		name string
		msgs []tea.Msg
		want int
	}{
		{"down", []tea.Msg{down}, 1},
		{"down twice", []tea.Msg{down, down}, 2},
		{"down then up", []tea.Msg{down, down, up}, 1},
		{"up at the top stays", []tea.Msg{up}, 0},
		{"week does not scroll", []tea.Msg{tuitest.Key("w"), down}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := append([]tea.Msg{bodySize, tuitest.Key("m"), tuitest.Key("l")}, tt.msgs...)
			m := tuitest.Drive(t, newModel(t, st), msgs...).(Model)
			if m.offset != tt.want {
				t.Errorf("offset = %d, want %d", m.offset, tt.want)
			}
		})
	}
}
