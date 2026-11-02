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

	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// now is the clock of the view tests: a Sunday noon at the end of the
// Activity fixture, so this week is 2026-W02 and last week 2026-W01 with
// the session that crosses midnight on New Year's Eve.
var now = time.Date(2026, 1, 11, 12, 0, 0, 0, testfixture.ActivityLocation)

func clock() time.Time { return now }

// bodySize is the window size the app shell hands the view at 100×30.
var bodySize = tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height - 4}

// work is the built-in working time: 09:00–17:00, 8h, 6h–9h per day and
// 32h–48h per week on target.
var work = store.WorkHours{Start: 9 * time.Hour, End: 17 * time.Hour, Target: 8 * time.Hour, Low: 6 * time.Hour, High: 9 * time.Hour, WeeklyLow: 32 * time.Hour, WeeklyHigh: 48 * time.Hour}

// holidays are those of Baden-Württemberg: the fixture has Weihnachten and
// Zweiter Weihnachtsfeiertag in last month, Neujahr in last week and
// Epiphanias (Tue 2026-01-06) in this week.
var holidays = holiday.New("Baden-Württemberg")

func newModel(t *testing.T, l Loader) Model {
	t.Helper()
	return New(Options{Loader: l, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work, Holidays: holidays, HumanOnly: true})
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
		{"LastWeekAll", []string{"l", "h"}},
		{"LastMonth", []string{"m", "l"}},
		{"Yesterday", []string{"d", "l"}},
		{"ShiftBack", []string{"["}},
		{"ShiftBackTwice", []string{"[", "["}},
		{"ShiftBackForth", []string{"[", "]"}},
		{"ShiftForwardEmpty", []string{"]"}},
		{"CurrentAfterShift", []string{"[", "[", "c"}},
		{"AllThenHuman", []string{"h", "h"}},
		{"DayKeepsAnchor", []string{"[", "d"}},
		{"MonthScroll", []string{"m", "l", "j", "j", "j"}},
		{"MonthScrollKeys", []string{"m", "l", "down", "down", "up"}},
		{"MonthScrollEnd", []string{"m", "l", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j"}},
		{"ScrollResetsOnWindow", []string{"m", "l", "j", "j", "]"}},
		{"UnknownKeyIgnored", []string{"x", "q", "1", "e"}},
		{"FiguresOff", []string{"s"}},
		{"FiguresBackOn", []string{"s", "s"}},
		{"MonthFiguresOff", []string{"m", "l", "s"}},
		{"HolidayDay", []string{"d", "[", "[", "[", "[", "["}},
		{"HolidayDayAll", []string{"d", "[", "[", "[", "[", "[", "h"}},
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
		{"TinyWeek", 40, 8, nil},
		{"LowWeek", 100, 11, nil},
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
		{"HolidayDayDark", color.Black, bodySize, []string{"d", "[", "[", "[", "[", "["}},
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
// column, no band, plain bars, the headline without a balance; the
// holidays are still named.
func TestNoWorkHours(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Holidays: holidays})
	m = drive(t, m, "m", "l")
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestNoHolidays renders this week without a holiday calendar: Tuesday is
// an ordinary workday, the week has five.
func TestNoHolidays(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work})
	m = drive(t, m)
	golden.RequireEqual(t, tuitest.Frame(m))
}

// holidayFunc adapts a function to store.Holidays.
type holidayFunc func(time.Time) string

func (f holidayFunc) Name(day time.Time) string { return f(day) }

// TestHolidayWithActivity marks days of this week with activity as
// holidays: Monday (activity early, the name follows the bar), Thursday
// (activity late, the name comes first) and the Sunday (a holiday on a
// weekend with activity: holiday colour and name). The week keeps three
// workdays.
func TestHolidayWithActivity(t *testing.T) {
	names := map[int]string{5: "Testtag", 8: "Fest der Prüfung", 11: "Sonntagsfeiertag"}
	st, _ := fixturedb.Activity(t)
	cal := holidayFunc(func(d time.Time) string { return names[d.Day()] })
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work, Holidays: cal})
	m = drive(t, m)
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestFutureDaysNoWorkdays renders this week as of Wednesday: Thursday
// and Friday are no workdays yet, so the target is two days (Monday and
// Wednesday; Tuesday is Epiphanias).
func TestFutureDaysNoWorkdays(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	wed := func() time.Time { return time.Date(2026, 1, 7, 15, 0, 0, 0, testfixture.ActivityLocation) }
	m := New(Options{Loader: st, Now: wed, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work, Holidays: holidays})
	m = drive(t, m)
	f := tuitest.Frame(m)
	if !strings.Contains(f, "target 16h00m (2 workdays × 8h00m)") {
		t.Errorf("workdays up to today not applied:\n%s", f)
	}
	golden.RequireEqual(t, f)
}

// TestProgram runs the view in a test program: the first load arrives
// asynchronously.
func TestProgram(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tm := tuitest.New(t, newModel(t, st))
	tuitest.WaitFor(t, tm, "week:")
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
		{"all events back and forth", []string{"h", "h", "h"}, 2},
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

// TestStartWindow: the view starts with the configured window.
func TestStartWindow(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	for name, p := range map[string]store.Period{"": store.PeriodWeek, "day": store.PeriodDay, "week": store.PeriodWeek, "month": store.PeriodMonth, "bogus": store.PeriodWeek} {
		m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, Window: name})
		if m.window.Period != p || m.window.Count != 1 {
			t.Errorf("Window %q: start = %+v", name, m.window)
		}
	}
}

// TestConfiguredKeys: the view acts on the configured keys only and shows
// them in its help.
func TestConfiguredKeys(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	km, err := keys.Resolve(map[string][]string{keys.ActivityEarlier: {"<"}, keys.ActivityLater: {">"}})
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, Keys: km})
	start := m.window
	if got := drive(t, m, "["); !got.window.Equal(start) {
		t.Errorf("[ still shifts the window")
	}
	if got := drive(t, m, "<", "<", ">"); !got.window.Equal(start.Shift(-1)) {
		t.Errorf("< and > do not shift: %v", got.window)
	}
	if h := m.keys.Range.Help(); h.Key != "</>" {
		t.Errorf("range group help = %q", h.Key)
	}
}
