package memtab

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/memview"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

func loaded(t *testing.T) Model {
	t.Helper()
	st, _ := fixturedb.Demo(t)
	var tm tea.Model = New(Options{Source: st, Location: demo.Location})
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 100, Height: 24})
	m, cmd := tm.(Model).Reload()
	return tuitest.Run(t, m, cmd).(Model)
}

func TestListsProjectsWithMemory(t *testing.T) {
	m := loaded(t)
	if len(m.rows) == 0 {
		t.Fatal("no projects")
	}
	out := m.View().Content
	for _, want := range []string{"kompass", "claude", "files"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	for i := 1; i < len(m.rows); i++ {
		if m.rows[i-1].Name > m.rows[i].Name {
			t.Errorf("rows not sorted by name: %q before %q", m.rows[i-1].Name, m.rows[i].Name)
		}
	}
}

func TestOpenSendsProject(t *testing.T) {
	tests := []struct {
		name string
		msgs []tea.Msg
	}{
		{"enter", []tea.Msg{tea.KeyPressMsg{Code: tea.KeyEnter}}},
		{"second click", []tea.Msg{tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 2}, tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tm tea.Model = loaded(t)
			var got tea.Msg
			for _, msg := range tt.msgs {
				var cmd tea.Cmd
				tm, cmd = tm.Update(msg)
				if cmd != nil {
					got = cmd()
				}
			}
			o, ok := got.(memview.Open)
			if !ok || o.ProjectID == 0 || o.Project != tm.(Model).rows[0].Name || o.Harness == "" {
				t.Errorf("got %#v", got)
			}
		})
	}
}

func TestMarkMoves(t *testing.T) {
	m := loaded(t)
	if len(m.rows) < 2 {
		t.Skip("one project")
	}
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if tm.(Model).cursor != 1 {
		t.Errorf("down: cursor %d", tm.(Model).cursor)
	}
	tm, cmd := tm.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 2})
	if tm.(Model).cursor != 0 || cmd != nil {
		t.Errorf("click on row 0: cursor %d", tm.(Model).cursor)
	}
}

func TestEmptyStates(t *testing.T) {
	var tm tea.Model = New(Options{})
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 80, Height: 10})
	tm, _ = tm.(Model).Reload()
	if !strings.Contains(tm.View().Content, "no database") {
		t.Errorf("no database:\n%s", tm.View().Content)
	}
}

type emptySource struct{}

func (emptySource) ProjectKeys(context.Context) ([]store.ProjectKey, error) { return nil, nil }
func (emptySource) MemoryProjects(context.Context) ([]store.MemoryProject, error) {
	return nil, nil
}

// TestEmptyArchiveExplains: without memory the tab says why and what to do.
func TestEmptyArchiveExplains(t *testing.T) {
	var tm tea.Model = New(Options{Source: emptySource{}})
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 100, Height: 10})
	m, cmd := tm.(Model).Reload()
	out := ansi.Strip(tuitest.Run(t, m, cmd).View().Content)
	for _, want := range []string{"No project has memory", "ingest --all", "ingest.store_memory"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty tab lacks %q:\n%s", want, out)
		}
	}
}
