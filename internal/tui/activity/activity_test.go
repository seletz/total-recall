package activity

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

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
		{"ShiftForwardStopsAtThisWeek", []string{"]"}},
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
		// ↑/↓ move the window by a day once the rows cannot scroll: a week
		// becomes a sliding window of seven days without an ISO week label.
		{"DayBack", []string{"k"}},
		{"DayBackArrow", []string{"up"}},
		{"DayBackForth", []string{"k", "k", "j"}},
		{"DayForwardStopsAtThisWeek", []string{"j", "down"}},
		{"DayBackToLastWeek", repeat("k", 7)},
		{"DayScrollsSingleDay", []string{"d", "k"}},
		{"DayScrollsSingleDayForthStops", []string{"d", "k", "j", "j"}},
		// Last month has 31 rows and 18 fit: 13 presses scroll to the end,
		// the 14th moves the window to Dec 2 – Jan 1 and keeps the length.
		{"MonthScrollThenShift", append([]string{"m", "l"}, repeat("j", 14)...)},
		{"MonthDayBackKeepsLength", []string{"m", "l", "k"}},
		// → after day steps jumps a period but not past this week.
		{"LaterClampedToThisWeek", []string{"k", "k", "]"}},
		{"LaterFromLastWeekDays", []string{"l", "k", "k", "]"}},
		{"CurrentAfterDays", []string{"k", "k", "k", "c"}},
		{"PeriodSwitchAfterDays", []string{"k", "k", "d"}},
		// o puts the newest day at the top; ↑/↓ still show the row above
		// and below, so ↓ now goes back in time and ↑ on, not past this
		// week. Flipping twice keeps the rows on screen.
		{"NewestTop", []string{"o"}},
		{"NewestTopDayBack", []string{"o", "j", "j"}},
		{"NewestTopUpStopsAtThisWeek", []string{"o", "k"}},
		{"NewestTopBackForth", []string{"o", "j", "j", "k"}},
		{"NewestTopMonthScroll", []string{"m", "l", "o", "j", "j"}},
		{"NewestTopMonthScrollEnd", append([]string{"m", "l", "o"}, repeat("j", 14)...)},
		{"NewestTopMonthUpShifts", []string{"m", "l", "o", "k"}},
		{"NewestTopFlipKeepsRows", []string{"m", "l", "j", "j", "o", "o"}},
		{"NewestTopFlipScrolled", []string{"m", "l", "j", "j", "o"}},
		{"NewestTopDay", []string{"d", "o"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), tt.keys...)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

// repeat returns the key name n times.
func repeat(k string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = k
	}
	return out
}

// TestDayStepBounds pins the window after day steps against the clock: ↑
// moves the first and the last day back by one, ↓ moves them on, and the
// window never ends after the current period, so mid-week the way back
// to this week stays open while ↓ in this week does nothing.
func TestDayStepBounds(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	wed := func() time.Time { return time.Date(2026, 1, 7, 15, 0, 0, 0, testfixture.ActivityLocation) }
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, testfixture.ActivityLocation) }
	tests := []struct {
		name         string
		now          func() time.Time
		keys         []string
		since, until time.Time
	}{
		{"sunday start", clock, nil, day(5), day(12)},
		{"sunday one back", clock, []string{"k"}, day(4), day(11)},
		{"sunday down stops", clock, []string{"j"}, day(5), day(12)},
		{"sunday back and forth", clock, []string{"k", "k", "k", "j"}, day(3), day(10)},
		{"sunday forth stops at this week", clock, []string{"k", "k", "j", "j", "j"}, day(5), day(12)},
		{"wednesday down stops", wed, []string{"j"}, day(5), day(12)},
		{"wednesday back and forth returns", wed, []string{"k", "k", "j", "j"}, day(5), day(12)},
		{"wednesday day view ends today", wed, []string{"d", "k", "j", "j"}, day(7), day(8)},
		{"wednesday later clamps", wed, []string{"k", "k", "k", "]"}, day(5), day(12)},
		{"wednesday later from last week", wed, []string{"l", "k", "]"}, day(4), day(11)},
		{"month back keeps length", clock, []string{"m", "k"}, time.Date(2025, 12, 31, 0, 0, 0, 0, testfixture.ActivityLocation), time.Date(2026, 1, 31, 0, 0, 0, 0, testfixture.ActivityLocation)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Options{Loader: st, Now: tt.now, Location: testfixture.ActivityLocation, DayStart: 4 * time.Hour, Work: work, Holidays: holidays, HumanOnly: true})
			m = drive(t, m, tt.keys...)
			if got := m.span.since(); !got.Equal(tt.since) {
				t.Errorf("since = %v, want %v", got, tt.since)
			}
			if got := m.span.until(); !got.Equal(tt.until) {
				t.Errorf("until = %v, want %v", got, tt.until)
			}
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

// countingLoader counts calls and can fail; projectCalls counts the
// project loads apart, projectErr fails them alone.
type countingLoader struct {
	st           *store.Store
	calls        atomic.Int32
	projectCalls atomic.Int32
	err          error
	projectErr   error
}

func (l *countingLoader) Workday(ctx context.Context, f store.WorkdayFilter) ([]store.Workday, error) {
	l.calls.Add(1)
	if l.err != nil {
		return nil, l.err
	}
	return l.st.Workday(ctx, f)
}

func (l *countingLoader) ProjectDays(ctx context.Context, f store.WorkdayFilter) ([]store.ProjectDay, error) {
	l.projectCalls.Add(1)
	if l.projectErr != nil {
		return nil, l.projectErr
	}
	return l.st.ProjectDays(ctx, f)
}

func TestLoadError(t *testing.T) {
	l := &countingLoader{err: errors.New("boom")}
	m := drive(t, newModel(t, l))
	golden.RequireEqual(t, tuitest.Frame(m))
}

// TestCache checks how often the days are queried: a load fetches the
// window with a period's worth of days (at least seven) in front and
// behind, so the neighbouring periods and the next day steps come from
// the cache; scrolling the rows never loads, a window seen before is
// shown without a command, and a held key fetches once per margin.
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
		{"day of the week is prefetched", []string{"d", "w"}, 1},
		{"last week is prefetched", []string{"l", "c", "l"}, 1},
		{"shift back and forth", []string{"[", "]"}, 1},
		{"two weeks back loads once", []string{"[", "[", "]", "]", "[", "["}, 2},
		{"all events back and forth", []string{"h", "h", "h"}, 2},
		{"month loads once, last month is prefetched", []string{"m", "l", "]", "l"}, 2},
		{"scrolling does not load", []string{"m", "j", "j", "k"}, 2},
		{"day steps within the margin", []string{"k", "j", "k"}, 1},
		{"day step at the limit does not load", []string{"j", "j"}, 1},
		{"seven days back come from the margin", repeat("k", 7), 1},
		{"held key fetches once per margin", repeat("k", 14), 2},
		{"held key over three margins", repeat("k", 21), 3},
		// A single day wants 21 lead days, more than the week's query fetched
		// in front of it.
		{"single day steps a week back", append([]string{"d"}, repeat("k", 7)...), 2},
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

// dayRow matches a day row of the frame.
var dayRow = regexp.MustCompile(`(?m)^[ ▎▸](Mon|Tue|Wed|Thu|Fri|Sat|Sun) \d{4}-\d{2}-\d{2} `)

// wheel repeats the wheel message n times.
func wheel(msg tea.MouseWheelMsg, n int) []tea.Msg {
	out := make([]tea.Msg, n)
	for i := range out {
		out[i] = msg
	}
	return out
}

// TestScrollKeepsContent sends day steps and wheel turns one message at a
// time and looks at the frame after every step before its command has
// run, the way a terminal shows them: every frame has the day rows, the
// headline and the figures of a complete window and the body height, no
// frame shows a loading or an empty state, and the frame changes only to
// a window whose days are all at hand; a step whose answer is pending
// leaves it as it was. Once the answers are in, the frame is the one the
// steps reach when every command runs at once.
func TestScrollKeepsContent(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	up := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}
	down := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	tests := []struct {
		name  string
		setup []string // keys driven to completion first
		msgs  []tea.Msg
		rows  int // day rows on screen: the body's 26 lines less the chrome
	}{
		{"keys", nil, tuitest.Keys(repeat("k", 10)...), 18},
		{"arrows", nil, tuitest.Keys(repeat("up", 16)...), 18},
		{"wheel", nil, wheel(up, 10), 18},
		{"wheel back and forth", nil, append(wheel(up, 9), wheel(down, 4)...), 18},
		{"periods", nil, tuitest.Keys("[", "[", "[", "]", "k", "k"), 18},
		{"month", []string{"m"}, wheel(up, 35), 18},
		{"month scrolled to the end", []string{"m", "l"}, append(tuitest.Keys(repeat("j", 13)...), wheel(down, 20)...), 18},
		{"single day", []string{"d"}, tuitest.Keys(repeat("k", 10)...), 22},
		{"newest at the top", []string{"o"}, tuitest.Keys(repeat("j", 10)...), 18},
		{"newest at the top, month", []string{"m", "o"}, append(tuitest.Keys(repeat("j", 13)...), wheel(down, 20)...), 18},
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
				if m.shown.span.period() != store.PeriodDay && !strings.Contains(f, "median") {
					t.Errorf("step %d lost the figures:\n%s", i, f)
				}
				if got := strings.Count(f, "\n"); got != bodySize.Height {
					t.Errorf("step %d has %d lines, want %d", i, got, bodySize.Height)
				}
				if f != prev && !m.shown.span.equal(m.span) {
					t.Errorf("step %d changed the frame before its answer arrived:\n%s", i, f)
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

// TestProgramScroll holds ↑ in a test program, where the answers arrive
// asynchronously: the output after the first complete frame (WaitFor
// consumes the output up to it) never paints a loading state, and the
// final frame is the one of the synchronous drive. The renderer paints a
// line from its first to its last changed cell, so the end of the window
// label is waited for: it is painted in one piece and names the last
// window only.
func TestProgramScroll(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tm := tuitest.New(t, newModel(t, st))
	tuitest.WaitFor(t, tm, "week:")
	tuitest.Press(tm, repeat("k", 20)...)
	tuitest.WaitFor(t, tm, "– 2025-12-22")
	_ = tm.Quit()
	out, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(tuitest.Timeout)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(out), []byte("loading")) {
		t.Error("a loading state was painted while scrolling")
	}
	want := tuitest.Drive(t, newModel(t, st), append([]tea.Msg{tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}}, tuitest.Keys(repeat("k", 20)...)...)...)
	final := tm.FinalModel(t, teatest.WithFinalTimeout(tuitest.Timeout)).(Model)
	if got, w := tuitest.Frame(final), tuitest.Frame(want); got != w {
		t.Errorf("final frame:\n%s\nwant:\n%s", got, w)
	}
}

// TestStaleResultDropped checks the two rules for late answers: one from
// before a reload belongs to a cache that is gone, so it is neither shown
// nor cached, however current its days look; one of the current
// generation fills the cache, even when its query was superseded, and is
// shown when it completes the window wanted now.
func TestStaleResultDropped(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := drive(t, newModel(t, st))
	before, cached := tuitest.Frame(m), len(m.cache)
	// Seven inactive days in place of this week: shown, the headline
	// would say 0 days.
	var days []store.Workday
	for day := m.span.since(); day.Before(m.span.until()); day = day.AddDate(0, 0, 1) {
		days = append(days, store.Workday{Day: day, Label: day.Format("2006-01-02")})
	}
	var tm tea.Model
	tm, _ = m.Update(resultMsg{gen: m.gen - 1, humanOnly: true, days: days})
	got := tm.(Model)
	if f := tuitest.Frame(got); f != before || len(got.cache) != cached {
		t.Errorf("an answer of an older generation changed the view or the cache:\n%s", f)
	}
	tm, _ = m.Update(resultMsg{gen: m.gen, id: 99, humanOnly: true, days: days})
	got = tm.(Model)
	if f := tuitest.Frame(got); !strings.Contains(f, "in 0 days") || len(got.cache) != cached {
		t.Errorf("a late answer of the current generation was not shown from the cache:\n%s", f)
	}
	// A reload cancels the running query; the error of the cancelled one
	// is not an error of the view.
	tm, first := m.Reload()
	tm, second := tm.(Model).Reload()
	tm, _ = tm.Update(first())
	if f := tuitest.Frame(tm); f != before {
		t.Errorf("the cancelled query changed the view:\n%s", f)
	}
	tm = tuitest.Run(t, tm, second)
	if f := tuitest.Frame(tm); f != before {
		t.Errorf("the reload changed the view:\n%s", f)
	}
}

// TestWheel checks that the mouse wheel acts like ↑/↓: it scrolls the day
// rows of last month and, at the top or when every row fits, moves the
// window by a day.
func TestWheel(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	down := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}
	tests := []struct {
		name   string
		msgs   []tea.Msg
		offset int
		shift  int
	}{
		{"down", []tea.Msg{down}, 1, 0},
		{"down twice", []tea.Msg{down, down}, 2, 0},
		{"down then up", []tea.Msg{down, down, up}, 1, 0},
		{"up at the top moves a day back", []tea.Msg{up}, 0, -1},
		{"week moves a day", []tea.Msg{tuitest.Key("c"), tuitest.Key("w"), up, up, down}, 0, -1},
		{"week does not move past today", []tea.Msg{tuitest.Key("c"), tuitest.Key("w"), down}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := append([]tea.Msg{bodySize, tuitest.Key("m"), tuitest.Key("l")}, tt.msgs...)
			m := tuitest.Drive(t, newModel(t, st), msgs...).(Model)
			if m.offset != tt.offset || m.span.shift != tt.shift {
				t.Errorf("offset = %d, shift = %d, want %d and %d", m.offset, m.span.shift, tt.offset, tt.shift)
			}
		})
	}
}

// TestStartWindow: the view starts with the configured window.
func TestStartWindow(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	for name, p := range map[string]store.Period{"": store.PeriodWeek, "day": store.PeriodDay, "week": store.PeriodWeek, "month": store.PeriodMonth, "bogus": store.PeriodWeek} {
		m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, Window: name})
		if m.span.window.Period != p || m.span.window.Count != 1 || m.span.shift != 0 {
			t.Errorf("Window %q: start = %+v", name, m.span)
		}
	}
}

// TestConfiguredKeys: the view acts on the configured keys only and shows
// them in its help.
func TestConfiguredKeys(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	km, err := keys.Resolve(map[string][]string{keys.ActivityEarlier: {"<"}, keys.ActivityLater: {">"}, keys.ActivityUp: {"u"}, keys.ActivityDown: {"n"}})
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, Keys: km})
	start := m.span
	if got := drive(t, m, "["); !got.span.equal(start) {
		t.Errorf("[ still shifts the window")
	}
	if got := drive(t, m, "<", "<", ">"); !got.span.equal(start.periods(-1)) {
		t.Errorf("< and > do not shift: %v", got.span)
	}
	if got := drive(t, m, "k", "up"); !got.span.equal(start) {
		t.Errorf("k and ↑ still move the window")
	}
	if got := drive(t, m, "u", "u", "n"); !got.span.equal(start.days(-1)) {
		t.Errorf("u and n do not move the window: %v", got.span)
	}
	if h := m.keys.Range.Help(); h.Key != "</>" {
		t.Errorf("range group help = %q", h.Key)
	}
	if h := m.keys.Up.Help(); h.Key != "u" {
		t.Errorf("up help = %q", h.Key)
	}
}

// switchX returns the column of label on the switches line.
func switchX(t *testing.T, m Model, label string) int {
	t.Helper()
	line, _, _, _ := m.switches()
	f := ansi.Strip(line)
	x := strings.Index(f, label)
	if x < 0 {
		t.Fatalf("switches %q lacks %q", f, label)
	}
	return ansi.StringWidth(f[:x])
}

// TestClick checks that a click on a period or counting mode of the
// switches line does what its key does, that a click on the summary
// figures hides them like s, and that clicks elsewhere do nothing.
func TestClick(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	click := func(x, y int) tea.MouseClickMsg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }
	// The figures take the three rows above the status line.
	figureY := bodySize.Height - 1 - figureRows
	tests := []struct {
		name    string
		keys    []string
		label   string // clicked on the switches line
		x, y    int    // clicked instead when label is empty
		period  store.Period
		human   bool
		figures bool
	}{
		{"day", nil, "day", 0, 0, store.PeriodDay, true, true},
		{"month", nil, "month", 0, 0, store.PeriodMonth, true, true},
		{"week from month", []string{"m"}, "week", 0, 0, store.PeriodWeek, true, true},
		{"all events", nil, "all", 0, 0, store.PeriodWeek, false, true},
		{"human from all", []string{"h"}, "human", 0, 0, store.PeriodWeek, true, true},
		{"active count is a no-op", nil, "human", 0, 0, store.PeriodWeek, true, true},
		{"active period is a no-op", nil, "week", 0, 0, store.PeriodWeek, true, true},
		{"label is inert", nil, "count:", 0, 0, store.PeriodWeek, true, true},
		{"figures hide", nil, "", 5, figureY, store.PeriodWeek, true, false},
		{"last figure row hides", nil, "", 5, figureY + figureRows - 1, store.PeriodWeek, true, false},
		{"rule above the figures is inert", nil, "", 5, figureY - 1, store.PeriodWeek, true, true},
		{"status line is inert", nil, "", 5, bodySize.Height - 1, store.PeriodWeek, true, true},
		{"day row is inert", nil, "", 5, 4, store.PeriodWeek, true, true},
		{"hidden figures stay hidden", []string{"s"}, "", 5, figureY, store.PeriodWeek, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, newModel(t, st), tt.keys...)
			x, y := tt.x, tt.y
			if tt.label != "" {
				x = switchX(t, m, tt.label)
			}
			m = tuitest.Send(t, m, click(x, y)).(Model)
			if m.span.period() != tt.period || m.humanOnly != tt.human || m.figures != tt.figures {
				t.Errorf("period = %v, humanOnly = %v, figures = %v; want %v, %v, %v", m.span.period(), m.humanOnly, m.figures, tt.period, tt.human, tt.figures)
			}
			if !m.shown.loaded {
				t.Error("not loaded after the click")
			}
		})
	}
	t.Run("figures of a day are no target", func(t *testing.T) {
		// A single day shows no figures; the rows above the status line
		// are day rows and padding.
		m := drive(t, newModel(t, st), "d")
		m = tuitest.Send(t, m, click(5, figureY)).(Model)
		if !m.figures {
			t.Error("figures switched off by a click on padding")
		}
	})
	t.Run("no database", func(t *testing.T) {
		m := drive(t, newModel(t, nil))
		m = tuitest.Send(t, m, click(switchX(t, m, "month"), 0)).(Model)
		if m.span.period() != store.PeriodWeek {
			t.Errorf("period = %v without a database", m.span.period())
		}
	})
}
