package activity

import (
	"context"
	"errors"
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

func newModel(t *testing.T, l Loader) Model {
	t.Helper()
	return New(Options{Loader: l, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour})
}

// run executes cmd synchronously and feeds every message it produces back
// into m, so a test can drive the view without a program.
func run(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(t, m, c)
		}
		return m
	default:
		var next tea.Cmd
		m, next = m.Update(msg)
		return run(t, m, next)
	}
}

// drive starts m with the body size and applies the key names in order,
// running every command to completion.
func drive(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	var tm tea.Model = m
	tm = run(t, tm, tm.Init())
	tm, _ = tm.Update(bodySize)
	for _, k := range keys {
		var cmd tea.Cmd
		tm, cmd = tm.Update(tuitest.Key(k))
		tm = run(t, tm, cmd)
	}
	return tm.(Model)
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
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation})
	m = drive(t, m, "l")
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
	tm = run(t, tm, cmd)
	tm, _ = tm.Update(stale)
	m = tm.(Model)
	if !strings.Contains(tuitest.Frame(m), "this week") || len(m.cache) != 2 {
		t.Errorf("stale result changed the view or was not cached:\n%s", tuitest.Frame(m))
	}
}

func TestFormatSpan(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{25*time.Minute + 5*time.Second, "25m"},
		{3*time.Hour + 5*time.Minute, "3h05m"},
		{26 * time.Hour, "26h00m"},
	}
	for _, tt := range tests {
		if got := formatSpan(tt.d); got != tt.want {
			t.Errorf("formatSpan(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
