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
