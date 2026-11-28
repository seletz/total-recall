package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/memview"
	"github.com/seletz/total-recall/internal/tui/session"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestMemoryTab: the memory is a tab of its own, reached by its key and by
// a click on its tab; enter on a project opens the viewer above the tab
// and esc returns to the tab, not to the search.
func TestMemoryTab(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	start := func(t *testing.T) App {
		t.Helper()
		app, err := New(Options{Store: st, Now: tuitest.Clock, Location: demo.Location})
		if err != nil {
			t.Fatal(err)
		}
		return tuitest.Drive(t, app, tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}).(App)
	}
	for _, via := range []string{"key", "click"} {
		t.Run(via, func(t *testing.T) {
			m := start(t)
			if via == "key" {
				m = tuitest.Drive(t, m, tuitest.Keys("tab", "5")...).(App)
			} else {
				m = tuitest.Send(t, m, tea.MouseClickMsg{X: tabX(t, m, "5 memory"), Y: 0, Button: tea.MouseLeft}).(App)
			}
			if m.active != 4 || m.showMemory {
				t.Fatalf("active = %d, viewer = %v", m.active, m.showMemory)
			}
			if out := ansi.Strip(m.View().Content); !strings.Contains(out, "kompass") || !strings.Contains(out, "files") {
				t.Fatalf("tab lacks the projects:\n%s", out)
			}
			m = tuitest.Send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}).(App)
			if !m.showMemory || m.active != 4 {
				t.Fatalf("enter: viewer = %v, active = %d", m.showMemory, m.active)
			}
			if h := ansi.Strip(m.header()); !strings.Contains(h, "5 memory") {
				t.Errorf("header = %q", h)
			}
			m = tuitest.Send(t, m, tea.KeyPressMsg{Code: tea.KeyEscape}).(App)
			if m.showMemory || m.active != 4 {
				t.Errorf("esc: viewer = %v, active = %d", m.showMemory, m.active)
			}
		})
	}
}

// TestSessionJumpsToMemory: m in the session viewer opens the memory of the
// project of the session and esc returns to the session; a session whose
// project has no memory says so and stays.
func TestSessionJumpsToMemory(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	app, err := New(Options{Store: st, Now: tuitest.Clock, Location: demo.Location})
	if err != nil {
		t.Fatal(err)
	}
	m := tuitest.Drive(t, app, tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}).(App)
	ps, err := st.MemoryProjects(t.Context())
	if err != nil || len(ps) == 0 {
		t.Fatalf("memory projects: %v, %v", ps, err)
	}
	var kompass store.MemoryProject
	for _, p := range ps {
		if strings.HasSuffix(p.Key, "-kompass") {
			kompass = p
		}
	}
	ref := session.OpenMemory{Key: kompass.GitRoot, Remote: kompass.GitRemoteURL}
	if ref.Key == "" {
		ref.Key, ref.Dir = kompass.Cwd, true
	}
	t.Run("with memory", func(t *testing.T) {
		got := tuitest.Send(t, m, ref).(App)
		if !got.showMemory || !got.memFromDetail {
			t.Fatalf("viewer = %v, fromDetail = %v", got.showMemory, got.memFromDetail)
		}
		got = tuitest.Send(t, got, memview.Back{}).(App)
		if got.showMemory || !got.showDetail {
			t.Errorf("back: viewer = %v, detail = %v", got.showMemory, got.showDetail)
		}
	})
	t.Run("without memory", func(t *testing.T) {
		got := tuitest.Send(t, m, session.OpenMemory{Key: "/nowhere", Dir: true}).(App)
		if got.showMemory {
			t.Error("viewer opened for a project without memory")
		}
	})
}

// TestFooterKeepsViewActions: in the session at 100 columns the footer
// names the actions of the view, M memory and esc back among them, before
// the global keys; nothing of the view is cut for the global ones.
func TestFooterKeepsViewActions(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	app, err := New(Options{Store: st, Query: "release", Now: tuitest.Clock, Location: demo.Location})
	if err != nil {
		t.Fatal(err)
	}
	m := tuitest.Drive(t, app, append([]tea.Msg{tea.WindowSizeMsg{Width: 100, Height: 30}}, tuitest.Keys("tab", "enter")...)...).(App)
	if !m.showDetail {
		t.Fatal("no session open")
	}
	line, _ := m.footer()
	f := ansi.Strip(line)
	for _, want := range []string{"M memory", "esc back", "T tools", "y copy id"} {
		if !strings.Contains(f, want) {
			t.Errorf("footer %q lacks %q", f, want)
		}
	}
	if i, j := strings.Index(f, "esc back"), strings.Index(f, "1 search"); j >= 0 && i > j {
		t.Errorf("global keys before the view's: %q", f)
	}
}

// TestHeaderFitsEightyColumns: at 80 columns the header holds the five tabs
// and the deepest layer name; the inactive tabs lose their names (2, 3, …)
// when the layer name leaves no room.
func TestHeaderFitsEightyColumns(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	app, err := New(Options{Store: st, Now: tuitest.Clock, Location: demo.Location})
	if err != nil {
		t.Fatal(err)
	}
	app.width = 80
	for name, set := range map[string]func(*App){
		"plain":              func(*App) {},
		"memory":             func(a *App) { a.showMemory = true },
		"session attachment": func(a *App) { a.showAttach, a.attachFromDetail = true, true },
		"day session":        func(a *App) { a.showDetail, a.detailFromDay = true, true },
	} {
		a := app
		set(&a)
		h := ansi.Strip(a.header())
		if w := ansi.StringWidth(h); w > 80 || strings.Contains(h, "\n") {
			t.Errorf("%s: header is %d cells: %q", name, w, h)
		}
		if !strings.Contains(h, " 5 ") {
			t.Errorf("%s: header lacks the last tab: %q", name, h)
		}
	}
}

// TestTabClickTargets: the click targets of the tab bar are wider than the
// labels (#376): a click up to three columns beside the tabs, and on the
// space between two tabs, still hits one of them; far left of the tabs it
// hits none.
func TestTabClickTargets(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	app, err := New(Options{Store: st, Now: tuitest.Clock, Location: demo.Location})
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{80, 120} {
		a := app
		a.width = width
		tabs := a.tabs()
		x := width - ansi.StringWidth(strings.Join(tabs, " ")) - 1
		first := x
		for i, tab := range tabs {
			w := ansi.StringWidth(tab)
			for _, c := range []int{x, x + w - 1} {
				if got := a.tabAt(c); got != i {
					t.Errorf("width %d: column %d is on tab %d, got %d", width, c, i, got)
				}
			}
			x += w + 1
		}
		for d := 1; d <= 3; d++ {
			if got := a.tabAt(first - d); got != 0 {
				t.Errorf("width %d: %d columns left of the first tab hit %d, want 0", width, d, got)
			}
		}
		if got := a.tabAt(width - 1); got != len(tabs)-1 {
			t.Errorf("width %d: the last column hit %d, want the last tab", width, got)
		}
		if got := a.tabAt(first - 4); got != -1 {
			t.Errorf("width %d: 4 columns left of the first tab hit %d, want none", width, got)
		}
		// Every column between the first and the last tab belongs to one.
		for c := first; c < width; c++ {
			if a.tabAt(c) < 0 {
				t.Errorf("width %d: column %d hits no tab", width, c)
			}
		}
	}
}
