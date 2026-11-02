package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestMouseMode checks that the view asks the terminal for mouse reports
// unless the mouse is switched off.
func TestMouseMode(t *testing.T) {
	tests := []struct {
		name    string
		noMouse bool
		want    tea.MouseMode
	}{
		{"default", false, tea.MouseModeCellMotion},
		{"no mouse", true, tea.MouseModeNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Now: tuitest.Clock, NoMouse: tt.noMouse})
			if err != nil {
				t.Fatal(err)
			}
			m := tuitest.Send(t, app, tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height})
			if got := m.View().MouseMode; got != tt.want {
				t.Errorf("MouseMode = %v, want %v", got, tt.want)
			}
		})
	}
}

// tabX returns the column of the label of tab i in the header.
func tabX(t *testing.T, a App, label string) int {
	t.Helper()
	h := ansi.Strip(a.header())
	x := strings.Index(h, label)
	if x < 0 {
		t.Fatalf("header %q lacks %q", h, label)
	}
	return x + 1
}

// TestMouseTabs checks that a click on a tab label switches the view and
// closes an open session, and that clicks elsewhere in the header do
// nothing.
func TestMouseTabs(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tests := []struct {
		name   string
		keys   []string // pressed first
		label  string   // clicked; empty clicks the program name
		active int
		detail bool
	}{
		{"stats", nil, "2 stats", 1, false},
		{"activity", nil, "3 activity", 2, false},
		{"search from stats", []string{"tab", "2"}, "1 search", 0, false},
		{"program name is not a tab", []string{"tab", "2"}, "", 1, false},
		{"tab click closes detail", []string{"enter"}, "1 search", 0, false},
		{"other tab closes detail", []string{"enter"}, "3 activity", 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Store: st, Query: "release", Now: tuitest.Clock, Location: testfixture.ActivityLocation})
			if err != nil {
				t.Fatal(err)
			}
			msgs := append([]tea.Msg{tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}}, tuitest.Keys(tt.keys...)...)
			m := tuitest.Drive(t, app, msgs...).(App)
			x := 1
			if tt.label != "" {
				x = tabX(t, m, tt.label)
			}
			m = tuitest.Send(t, m, tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft}).(App)
			if m.active != tt.active || m.showDetail != tt.detail {
				t.Errorf("active = %d, detail = %v; want %d, %v", m.active, m.showDetail, tt.active, tt.detail)
			}
		})
	}
}

// TestMouseBody checks that mouse messages in the body reach the current
// view with the header rows subtracted, that the chrome and the help
// overlay swallow them, and that the detail gets them while it is shown.
func TestMouseBody(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	open := func(t *testing.T, keys ...string) App {
		t.Helper()
		app, err := New(Options{Store: st, Query: "release", Now: tuitest.Clock, Location: testfixture.ActivityLocation})
		if err != nil {
			t.Fatal(err)
		}
		msgs := append([]tea.Msg{tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}}, tuitest.Keys(keys...)...)
		return tuitest.Drive(t, app, msgs...).(App)
	}
	// Row headerRows+2 is the first hit of the search view; the second
	// hit starts two rows below.
	secondHit := headerRows + 2 + 2
	t.Run("click marks a hit", func(t *testing.T) {
		m := tuitest.Send(t, open(t), tea.MouseClickMsg{X: 5, Y: secondHit, Button: tea.MouseLeft}).(App)
		if !strings.Contains(tuitest.Frame(m), "\n▸") && !strings.Contains(ansi.Strip(m.views[0].View().Content), "▸") {
			t.Fatalf("no mark in frame:\n%s", tuitest.Frame(m))
		}
		if f, ok := m.views[0].(Focused); !ok || f.Focused() {
			t.Errorf("list should hold the focus after a click")
		}
	})
	t.Run("rule above the body is ignored", func(t *testing.T) {
		m := tuitest.Send(t, open(t), tea.MouseClickMsg{X: 5, Y: headerRows - 1, Button: tea.MouseLeft}).(App)
		if f, ok := m.views[0].(Focused); !ok || !f.Focused() {
			t.Errorf("input should keep the focus")
		}
	})
	t.Run("footer is ignored", func(t *testing.T) {
		m := tuitest.Send(t, open(t), tea.MouseClickMsg{X: 5, Y: tuitest.Height - 1, Button: tea.MouseLeft}).(App)
		if f, ok := m.views[0].(Focused); !ok || !f.Focused() {
			t.Errorf("input should keep the focus")
		}
	})
	t.Run("help swallows clicks", func(t *testing.T) {
		m := tuitest.Send(t, open(t, "tab", "?"), tea.MouseClickMsg{X: tabX(t, open(t), "2 stats"), Y: 0, Button: tea.MouseLeft}).(App)
		if !m.help || m.active != 0 {
			t.Errorf("help = %v, active = %d; want help open on search", m.help, m.active)
		}
	})
	t.Run("wheel scrolls the detail", func(t *testing.T) {
		m := open(t, "enter")
		if !m.showDetail {
			t.Fatal("enter did not open the detail")
		}
		// Low enough that the session does not fit.
		m = tuitest.Send(t, m, tea.WindowSizeMsg{Width: tuitest.Width, Height: 10}).(App)
		before := tuitest.Frame(m)
		m = tuitest.Send(t, m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}).(App)
		if after := tuitest.Frame(m); after == before {
			t.Errorf("wheel did not scroll the detail:\n%s", after)
		}
	})
}
