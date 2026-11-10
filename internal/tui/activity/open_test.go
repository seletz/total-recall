package activity

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/dayview"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// opened returns the day the view asks to open for msg, run on m.
func opened(t *testing.T, m Model, msg tea.Msg) (dayview.Open, bool) {
	t.Helper()
	_, cmd := m.Update(msg)
	if cmd == nil {
		return dayview.Open{}, false
	}
	o, ok := cmd().(dayview.Open)
	return o, ok
}

func TestOpenDay(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	base := drive(t, newModel(t, st))
	day := func(o dayview.Open) string { return o.Day.Format("2006-01-02") }

	// The selection starts on the newest day of the window: the week of
	// 2026-01-11 (a Sunday), counted as human events only.
	o, ok := opened(t, base, tuitest.Key("enter"))
	if !ok || day(o) != "2026-01-11" || !o.HumanOnly {
		t.Fatalf("enter: %v %+v", ok, o)
	}
	// J/K move the selection, enter opens that day.
	for _, tc := range []struct {
		keys []string
		want string
	}{
		{[]string{"K"}, "2026-01-10"},
		{[]string{"K", "K", "K"}, "2026-01-08"},
		{[]string{"K", "J"}, "2026-01-11"},
		{[]string{"shift+up", "shift+up"}, "2026-01-09"},
	} {
		m := drive(t, newModel(t, st), tc.keys...)
		if o, ok := opened(t, m, tuitest.Key("enter")); !ok || day(o) != tc.want {
			t.Errorf("%v then enter: %v %s, want %s", tc.keys, ok, day(o), tc.want)
		}
	}
	// The counting mode of the view goes along.
	m := drive(t, newModel(t, st), "h")
	if o, ok := opened(t, m, tuitest.Key("enter")); !ok || o.HumanOnly {
		t.Errorf("all events: %v %+v", ok, o)
	}
	// The selected row carries ▸.
	f := tuitest.Frame(drive(t, newModel(t, st), "K"))
	if !strings.Contains(f, "▸Sat 2026-01-10") || strings.Contains(f, "▸Sun") {
		t.Errorf("selection not marked:\n%s", f)
	}
}

func TestOpenDayByClick(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := drive(t, newModel(t, st))
	// Row y of the frame: switches, headline, header, then the days.
	for y, line := range strings.Split(tuitest.Frame(m), "\n") {
		if !strings.Contains(line, "2026-01-08") {
			continue
		}
		o, ok := opened(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
		if !ok || o.Day.Format("2006-01-02") != "2026-01-08" {
			t.Errorf("click on row %d: %v %+v", y, ok, o)
		}
		// a click on the header line does not open anything
		if _, ok := opened(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 40, Y: m.headerY()}); ok {
			t.Error("click on the header opened a day")
		}
		return
	}
	t.Fatal("no row of 2026-01-08")
}

func TestSelectMessageAndScroll(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := drive(t, newModel(t, st))
	tm, _ := m.Update(Select{Day: time.Date(2026, 1, 9, 0, 0, 0, 0, testfixture.ActivityLocation)})
	if o, ok := opened(t, tm.(Model), tuitest.Key("enter")); !ok || o.Day.Format("2006-01-02") != "2026-01-09" {
		t.Errorf("select message: %v %+v", ok, o)
	}
	// Scrolling the rows away leaves the selection at the edge it left by.
	s := drive(t, newModel(t, st), repeat("k", 12)...)
	if o, ok := opened(t, s, tuitest.Key("enter")); !ok || o.Day.IsZero() {
		t.Errorf("after scrolling: %v %+v", ok, o)
	}
}
