package stats

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
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// now is the clock of the view tests: a Sunday noon at the end of the
// Activity fixture, so the default windows of every period contain data.
var now = time.Date(2026, 1, 11, 12, 0, 0, 0, testfixture.ActivityLocation)

func clock() time.Time { return now }

// bodySize is the window size the app shell hands the view at 100×30.
var bodySize = tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height - 4}

func newModel(t *testing.T, l Loader) Model {
	t.Helper()
	return New(Options{Loader: l, Now: clock, Location: testfixture.ActivityLocation})
}

// run executes cmd synchronously and feeds every message it produces back
// into m, so a test can drive the view without a program: a load command
// yields its result right away, batches are unpacked.
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

// TestFrames drives the view through every switch against the Activity
// fixture and compares the frame with a golden file. Commands run
// synchronously, so every frame shows the loaded state.
func TestFrames(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"Start", nil},
		{"Day", []string{"w", "d"}},
		{"Week", []string{"w"}},
		{"Month", []string{"m"}},
		{"Project", []string{"a", "p"}},
		{"Agent", []string{"a"}},
		{"Directory", []string{"v"}},
		{"WeekAgent", []string{"w", "a"}},
		{"ShiftBack", []string{"["}},
		{"ShiftBackTwice", []string{"[", "["}},
		{"ShiftBackForth", []string{"[", "]"}},
		{"ShiftForwardEmpty", []string{"]"}},
		{"ZoomOut", []string{"-"}},
		{"ZoomOutTwice", []string{"-", "-"}},
		{"ZoomIn", []string{"+"}},
		{"ZoomInEquals", []string{"="}},
		{"MonthZoomOut", []string{"m", "-", "-"}},
		{"MetricHuman", []string{"e"}},
		{"MetricSessions", []string{"e", "e"}},
		{"MetricWraps", []string{"e", "e", "e"}},
		{"WeekShiftBackMonth", []string{"w", "[", "m"}},
		{"UnknownKeyIgnored", []string{"x", "q", "1"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), tt.keys...)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

// TestProgram runs the view in a test program: the first load arrives
// asynchronously and the layout follows the window size.
func TestProgram(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
	}{
		{"Start", 0, 0},
		// The app shell at 80×24 gives the body 20 rows.
		{"Narrow", 80, 20},
		{"Tiny", 40, 8},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tuitest.New(t, newModel(t, st))
			tuitest.WaitFor(t, tm, "alpha")
			if tt.width > 0 {
				tuitest.Resize(tm, tt.width, tt.height)
			}
			final := tuitest.Final(t, tm).(Model)
			golden.RequireEqual(t, tuitest.Frame(final))
		})
	}
}

// TestNoStore renders the hint shown without a database; keys do nothing.
func TestNoStore(t *testing.T) {
	tm := tuitest.New(t, newModel(t, nil))
	tuitest.WaitFor(t, tm, "no database")
	tuitest.Press(tm, "w", "a", "[")
	final := tuitest.Final(t, tm).(Model)
	golden.RequireEqual(t, tuitest.Frame(final))
}

// countingLoader counts calls and can fail.
type countingLoader struct {
	st    *store.Store
	calls atomic.Int32
	err   error
}

func (l *countingLoader) ActivitySeries(ctx context.Context, f store.ActivityFilter) (store.ActivitySeries, error) {
	l.calls.Add(1)
	if l.err != nil {
		return store.ActivitySeries{}, l.err
	}
	return l.st.ActivitySeries(ctx, f)
}

func TestLoadError(t *testing.T) {
	l := &countingLoader{err: errors.New("boom")}
	m := drive(t, newModel(t, l))
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestCache checks that a filter combination is loaded once: switching back
// to a seen period, dimension or window shows the cached series without a
// command, while the metric never loads.
func TestCache(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tests := []struct {
		name  string
		keys  []string
		loads int32
	}{
		{"start", nil, 1},
		{"same period again", []string{"d"}, 1},
		{"period back and forth", []string{"w", "d"}, 2},
		{"all periods", []string{"w", "m", "d", "w", "m"}, 3},
		{"dimension back and forth", []string{"a", "p", "a"}, 2},
		{"shift back and forth", []string{"[", "]"}, 2},
		{"zoom out and in", []string{"-", "+"}, 2},
		{"metric does not load", []string{"e", "e", "e"}, 1},
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

// TestStaleResult checks that the answer of an outdated load is dropped:
// after pressing w and m before any answer arrives, the week load is
// cancelled and its answer (an error, or data) must neither show nor leave
// an error behind; the month result must show. A later w loads the week
// again.
func TestStaleResult(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	l := &countingLoader{st: st}
	var m tea.Model = newModel(t, l)
	m = run(t, m, m.Init())
	m, _ = m.Update(bodySize)
	m, weekCmd := m.Update(tuitest.Key("w"))
	m, monthCmd := m.Update(tuitest.Key("m"))
	if weekCmd == nil || monthCmd == nil {
		t.Fatal("expected a load command for w and m")
	}
	m, _ = m.Update(weekCmd())
	if f := tuitest.Frame(m); strings.Contains(f, "W02") || strings.Contains(f, "error") || !strings.Contains(f, "loading") {
		t.Errorf("stale week result applied:\n%s", f)
	}
	m, _ = m.Update(monthCmd())
	if f := tuitest.Frame(m); !strings.Contains(f, "Jan") || strings.Contains(f, "loading") {
		t.Errorf("month result not applied:\n%s", f)
	}
	m, cmd := m.Update(tuitest.Key("w"))
	if cmd == nil {
		t.Fatal("cancelled week load was not repeated")
	}
	m, _ = m.Update(cmd())
	if f := tuitest.Frame(m); !strings.Contains(f, "W02") {
		t.Errorf("week not shown:\n%s", f)
	}
	if got := l.calls.Load(); got != 4 {
		t.Errorf("loads = %d, want 4", got)
	}
}

// TestKeyMap checks the bindings the footer and the help overlay show.
func TestKeyMap(t *testing.T) {
	m := newModel(t, nil)
	short := m.ShortHelp()
	if len(short) != 3 {
		t.Errorf("ShortHelp has %d bindings, want 3", len(short))
	}
	var keys []string
	for _, group := range m.FullHelp() {
		for _, b := range group {
			keys = append(keys, b.Keys()...)
		}
	}
	want := []string{"d", "w", "m", "p", "a", "v", "[", "]", "-", "+", "=", "e"}
	for _, k := range want {
		found := false
		for _, g := range keys {
			if g == k {
				found = true
			}
		}
		if !found {
			t.Errorf("key %q missing from FullHelp %v", k, keys)
		}
	}
	// None of the view's keys may be one the app shell handles itself.
	for _, k := range keys {
		switch k {
		case "q", "1", "2", "tab", "?", "esc", "ctrl+c":
			t.Errorf("key %q collides with the app shell", k)
		}
	}
}

// TestChartContainer checks the seam for other renderers: a container set
// through Options gets the spec and the size and draws the chart area.
func TestChartContainer(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	c := &fakeChart{}
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, Chart: c})
	m = drive(t, m, "w")
	if c.spec.Period != store.PeriodWeek || len(c.spec.Series) != 3 {
		t.Errorf("container spec = period %s, %d series; want week, 3", c.spec.Period, len(c.spec.Series))
	}
	if c.width != tuitest.Width-1 || c.height < MinChartHeight {
		t.Errorf("container size = %d×%d", c.width, c.height)
	}
	f := tuitest.Frame(m)
	if !strings.Contains(f, "fake chart") || !strings.Contains(f, "fake mode") {
		t.Errorf("frame does not show the container:\n%s", f)
	}
	// Messages the model does not consume reach the container.
	type probeMsg struct{}
	m.Update(probeMsg{})
	if c.updates != 1 {
		t.Errorf("container got %d messages, want 1", c.updates)
	}
}

type fakeChart struct {
	spec          chart.Spec
	width, height int
	updates       int
}

func (c *fakeChart) Init() tea.Cmd                   { return nil }
func (c *fakeChart) Update(tea.Msg) (Chart, tea.Cmd) { c.updates++; return c, nil }
func (c *fakeChart) SetSpec(s chart.Spec)            { c.spec = s }
func (c *fakeChart) SetSize(w, h int)                { c.width, c.height = w, h }
func (c *fakeChart) View() string                    { return "fake chart" }
func (c *fakeChart) Mode() string                    { return "fake mode" }
