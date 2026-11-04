package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
	"github.com/seletz/total-recall/internal/tui/zone"
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
	t.Run("rule below the body is ignored", func(t *testing.T) {
		m := tuitest.Send(t, open(t), tea.MouseClickMsg{X: 5, Y: tuitest.Height - 2, Button: tea.MouseLeft}).(App)
		if f, ok := m.views[0].(Focused); !ok || !f.Focused() {
			t.Errorf("input should keep the focus")
		}
	})
	t.Run("help keeps wheel and inside clicks", func(t *testing.T) {
		m := open(t, "tab", "?")
		r := helpRect(t, m)
		m = tuitest.Send(t, m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}).(App)
		m = tuitest.Send(t, m, tea.MouseClickMsg{X: r.X + 1, Y: r.Y + 1, Button: tea.MouseLeft}).(App)
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

// footerX returns the column of label in the footer.
func footerX(t *testing.T, a App, label string) int {
	t.Helper()
	line, _ := a.footer()
	f := ansi.Strip(line)
	x := strings.Index(f, label)
	if x < 0 {
		t.Fatalf("footer %q lacks %q", f, label)
	}
	return ansi.StringWidth(f[:x])
}

// helpRect returns the rectangle of the help panel in screen cells, found
// by its corners in the frame.
func helpRect(t *testing.T, a App) zone.Rect {
	t.Helper()
	lines := strings.Split(tuitest.Frame(a), "\n")
	r := zone.Rect{X: -1}
	col := func(l, s string) int { return ansi.StringWidth(l[:strings.Index(l, s)]) }
	for y, l := range lines {
		if strings.Contains(l, "╭") {
			r.X, r.Y, r.W = col(l, "╭"), y, col(l, "╮")-col(l, "╭")+1
		}
		if strings.Contains(l, "╰") {
			r.H = y - r.Y + 1
		}
	}
	if r.X < 0 || r.W <= 0 || r.H <= 0 {
		t.Fatalf("no panel in frame:\n%s", tuitest.Frame(a))
	}
	return r
}

// TestMouseFooter checks that a click on a key hint in the footer presses
// its key: a global key switches the view or opens the help, a view key
// goes to the current view, each key of a grouped hint on its own, and a
// hint cut off by a narrow terminal does nothing.
func TestMouseFooter(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	open := func(t *testing.T, width int, keys ...string) App {
		t.Helper()
		app, err := New(Options{Store: st, Query: "release", Now: tuitest.Clock, Location: testfixture.ActivityLocation})
		if err != nil {
			t.Fatal(err)
		}
		msgs := append([]tea.Msg{tea.WindowSizeMsg{Width: width, Height: tuitest.Height}}, tuitest.Keys(keys...)...)
		return tuitest.Drive(t, app, msgs...).(App)
	}
	click := func(t *testing.T, m App, x int) App {
		t.Helper()
		return tuitest.Send(t, m, tea.MouseClickMsg{X: x, Y: tuitest.Height - 1, Button: tea.MouseLeft}).(App)
	}
	t.Run("global key switches the view", func(t *testing.T) {
		m := open(t, tuitest.Width, "tab", "2")
		m = click(t, m, footerX(t, m, "3 activity"))
		if m.active != 2 {
			t.Errorf("active = %d, want activity", m.active)
		}
	})
	t.Run("help opens and closes", func(t *testing.T) {
		m := open(t, tuitest.Width, "tab", "2")
		m = click(t, m, footerX(t, m, "? help"))
		if !m.help {
			t.Fatal("help did not open")
		}
		m = click(t, m, footerX(t, m, "esc back"))
		if m.help {
			t.Error("help did not close")
		}
	})
	t.Run("view key reaches the view", func(t *testing.T) {
		m := open(t, tuitest.Width)
		m = click(t, m, footerX(t, m, "tab list"))
		if f, ok := m.views[0].(Focused); !ok || f.Focused() {
			t.Error("list should hold the focus")
		}
	})
	t.Run("each key of a group", func(t *testing.T) {
		m := open(t, tuitest.Width, "tab", "2")
		x := footerX(t, m, "d/w/m period")
		m = click(t, m, x+2) // w
		if f := tuitest.Frame(m); !strings.Contains(f, "[week]") {
			t.Errorf("w did not switch to weeks:\n%s", f)
		}
		m = click(t, m, x+4) // m
		if f := tuitest.Frame(m); !strings.Contains(f, "[month]") {
			t.Errorf("m did not switch to months:\n%s", f)
		}
	})
	t.Run("label of a group is inert", func(t *testing.T) {
		m := open(t, tuitest.Width, "tab", "2")
		m = click(t, m, footerX(t, m, "d/w/m period")+8)
		if f := tuitest.Frame(m); !strings.Contains(f, "[day]") {
			t.Errorf("period label changed the period:\n%s", f)
		}
	})
	t.Run("label of a single key counts", func(t *testing.T) {
		m := open(t, tuitest.Width, "enter")
		if !m.showDetail {
			t.Fatal("enter did not open the detail")
		}
		m = click(t, m, footerX(t, m, "esc back")+5)
		if m.showDetail {
			t.Error("back did not close the detail")
		}
	})
	t.Run("quit", func(t *testing.T) {
		m := open(t, tuitest.Width, "tab", "2")
		_, cmd := m.Update(tea.MouseClickMsg{X: footerX(t, m, "q quit"), Y: tuitest.Height - 1, Button: tea.MouseLeft})
		if cmd == nil {
			t.Fatal("no command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Error("q did not quit")
		}
	})
	t.Run("cut-off hint is inert", func(t *testing.T) {
		// The session footer does not fit in 100 columns: the global keys
		// after "3 activity" are cut, so a click at the end of the line
		// does not open the help or quit.
		m := open(t, tuitest.Width, "enter")
		line, _ := m.footer()
		if f := ansi.Strip(line); !strings.HasSuffix(f, "…") || strings.Contains(f, "q quit") {
			t.Fatalf("footer should be cut: %q", f)
		}
		_, cmd := m.Update(tea.MouseClickMsg{X: tuitest.Width - 1, Y: tuitest.Height - 1, Button: tea.MouseLeft})
		if m2 := tuitest.Run(t, m, cmd).(App); m2.help || !m2.showDetail {
			t.Errorf("help = %v, detail = %v after a click on the ellipsis", m2.help, m2.showDetail)
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Error("click on the ellipsis quit")
			}
		}
	})
	t.Run("between hints is inert", func(t *testing.T) {
		m := open(t, tuitest.Width, "tab", "2")
		m = click(t, m, footerX(t, m, "• 3 activity")+1)
		if m.active != 1 {
			t.Errorf("active = %d after a click on the separator", m.active)
		}
	})
	t.Run("right button is ignored", func(t *testing.T) {
		m := open(t, tuitest.Width, "tab", "2")
		m = tuitest.Send(t, m, tea.MouseClickMsg{X: footerX(t, m, "3 activity"), Y: tuitest.Height - 1, Button: tea.MouseRight}).(App)
		if m.active != 1 {
			t.Errorf("active = %d after a right click", m.active)
		}
	})
}

// TestMouseHelp checks that a click outside the help panel closes the help
// and does nothing else, while a click inside keeps it open.
func TestMouseHelp(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	open := func(t *testing.T) App {
		t.Helper()
		app, err := New(Options{Store: st, Query: "release", Now: tuitest.Clock, Location: testfixture.ActivityLocation})
		if err != nil {
			t.Fatal(err)
		}
		return tuitest.Drive(t, app, tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}, tuitest.Key("tab"), tuitest.Key("?")).(App)
	}
	m := open(t)
	r := helpRect(t, m)
	tests := []struct {
		name string
		x, y int
		open bool
	}{
		{"top-left corner", r.X, r.Y, true},
		{"bottom-right corner", r.X + r.W - 1, r.Y + r.H - 1, true},
		{"inside", r.X + 3, r.Y + 2, true},
		{"left of the panel", r.X - 1, r.Y + 2, false},
		{"right of the panel", r.X + r.W, r.Y + 2, false},
		{"above the panel", r.X + 3, r.Y - 1, false},
		{"below the panel", r.X + 3, r.Y + r.H, false},
		{"header", 1, 0, false},
		{"tab in the header", tabX(t, m, "2 stats"), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tuitest.Send(t, open(t), tea.MouseClickMsg{X: tt.x, Y: tt.y, Button: tea.MouseLeft}).(App)
			if got.help != tt.open {
				t.Errorf("help = %v, want %v", got.help, tt.open)
			}
			if got.active != 0 {
				t.Errorf("active = %d, want search", got.active)
			}
		})
	}
}
