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

// drive starts m with the body size and applies the key names in order,
// running every command to completion (see tuitest.Drive).
func drive(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	msgs := append([]tea.Msg{bodySize}, tuitest.Keys(keys...)...)
	return tuitest.Drive(t, m, msgs...).(Model)
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
		{"Heatmap", []string{"h"}},
		{"HeatmapOff", []string{"h", "h"}},
		{"HeatmapWeek", []string{"h", "w"}},
		{"HeatmapMonthZoomOut", []string{"h", "m", "-"}},
		{"HeatmapHuman", []string{"h", "e"}},
		{"HeatmapEmpty", []string{"h", "]"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), tt.keys...)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

// TestHeatmapLayout checks the split of the body with the heatmap on: the
// table shrinks first, the bar chart keeps MinChartHeight, and when even
// that leaves no room the heatmap is left out and the status line says so.
func TestHeatmapLayout(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
	}{
		{"Full", tuitest.Width, tuitest.Height - 4},
		{"TableShrinks", tuitest.Width, 24},
		{"NoRoom", 80, 20},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), "h")
			var tm tea.Model
			tm, _ = m.Update(tea.WindowSizeMsg{Width: tt.width, Height: tt.height})
			m = tm.(Model)
			f := tuitest.Frame(m)
			if got := strings.Count(f, "\n"); got != tt.height {
				t.Errorf("frame has %d lines, want %d:\n%s", got, tt.height, f)
			}
			golden.RequireEqual(t, f)
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

// countingLoader counts calls per query and can fail.
type countingLoader struct {
	st    *store.Store
	calls atomic.Int32 // ActivitySeries
	stats atomic.Int32 // Stats
	err   error
}

func (l *countingLoader) Stats(ctx context.Context, f store.StatsFilter) (store.Stats, error) {
	l.stats.Add(1)
	if l.err != nil {
		return store.Stats{}, l.err
	}
	return l.st.Stats(ctx, f)
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

// TestHeatmapLoadError shows the series and the error of the heatmap load
// in its place: the series query succeeded, only the stats query failed.
func TestHeatmapLoadError(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	l := &countingLoader{st: st}
	m := drive(t, newModel(t, l))
	l.err = errors.New("boom")
	tm := tuitest.Send(t, m, tuitest.Key("h"))
	golden.RequireEqual(t, tuitest.Frame(tm))
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
		stats int32
	}{
		{"start", nil, 1, 0},
		{"same period again", []string{"d"}, 1, 0},
		{"period back and forth", []string{"w", "d"}, 2, 0},
		{"all periods", []string{"w", "m", "d", "w", "m"}, 3, 0},
		{"dimension back and forth", []string{"a", "p", "a"}, 2, 0},
		{"shift back and forth", []string{"[", "]"}, 2, 0},
		{"zoom out and in", []string{"-", "+"}, 2, 0},
		{"metric does not load", []string{"e", "e", "e"}, 1, 0},
		{"heatmap loads once", []string{"h"}, 1, 1},
		{"heatmap off and on is cached", []string{"h", "h", "h"}, 1, 1},
		{"heatmap follows the window", []string{"h", "[", "]"}, 2, 2},
		{"heatmap ignores the dimension", []string{"h", "a", "v"}, 3, 1},
		{"heatmap ignores the metric", []string{"h", "e"}, 1, 1},
		{"heatmap off does not load", []string{"h", "h", "["}, 2, 1},
		{"heatmap on reuses the series cache", []string{"[", "]", "h"}, 2, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &countingLoader{st: st}
			drive(t, newModel(t, l), tt.keys...)
			if got := l.calls.Load(); got != tt.loads {
				t.Errorf("series loads = %d, want %d", got, tt.loads)
			}
			if got := l.stats.Load(); got != tt.stats {
				t.Errorf("stats loads = %d, want %d", got, tt.stats)
			}
		})
	}
}

// TestStaleHeatmap checks that the answer of an outdated heatmap load is
// dropped: after h and [ before any answer arrives, only the shifted
// window's heatmap shows, and switching the heatmap off cancels its load.
func TestStaleHeatmap(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	l := &countingLoader{st: st}
	var m tea.Model = newModel(t, l)
	m = tuitest.Run(t, m, m.Init())
	m, _ = m.Update(bodySize)
	m, onCmd := m.Update(tuitest.Key("h"))
	m, shiftCmd := m.Update(tuitest.Key("["))
	if onCmd == nil || shiftCmd == nil {
		t.Fatal("expected load commands for h and [")
	}
	m, _ = m.Update(onCmd())
	if f := tuitest.Frame(m); !strings.Contains(f, "Loading heatmap") || strings.Contains(f, "max ") {
		t.Errorf("stale heatmap applied:\n%s", f)
	}
	m = tuitest.Run(t, m, shiftCmd)
	if f := tuitest.Frame(m); strings.Contains(f, "loading") || !strings.Contains(f, "weekday × hour") {
		t.Errorf("shifted heatmap not shown:\n%s", f)
	}
	m, _ = m.Update(tuitest.Key("]"))
	m, cmd := m.Update(tuitest.Key("h"))
	if cmd != nil {
		t.Error("switching the heatmap off started a command")
	}
	if f := tuitest.Frame(m); strings.Contains(f, "heatmap") {
		t.Errorf("heatmap still shown after h:\n%s", f)
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
	m = tuitest.Run(t, m, m.Init())
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
	want := []string{"d", "w", "m", "p", "a", "v", "[", "]", "-", "+", "=", "e", "h"}
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
func (c *fakeChart) SetSpec(s chart.Spec) tea.Cmd    { c.spec = s; return nil }
func (c *fakeChart) SetSize(w, h int) tea.Cmd        { c.width, c.height = w, h; return nil }
func (c *fakeChart) View() string                    { return "fake chart" }
func (c *fakeChart) Mode() string                    { return "fake mode" }
