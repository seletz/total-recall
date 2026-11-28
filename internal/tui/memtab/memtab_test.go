package memtab

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/project"
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

// realSource has the shapes of a real archive (#375): one configured
// project with two memory directories, a directory whose working directory
// is unknown (only the raw key of the harness), a project without any
// file, and one whose files are all deleted.
type realSource struct{ alt string }

func (realSource) ProjectKeys(context.Context) ([]store.ProjectKey, error) { return nil, nil }
func (r realSource) MemoryProjects(context.Context) ([]store.MemoryProject, error) {
	at := time.Date(2026, 10, 31, 22, 40, 0, 0, time.UTC)
	return []store.MemoryProject{
		{ID: 1, Harness: "claude", Key: "-home-mara-src-kompass", Cwd: "/home/mara/src/kompass", GitRoot: "/home/mara/src/kompass", Files: 3, Versions: 7, LastChange: at},
		{ID: 2, Harness: "claude", Key: "-home-mara-src-kompass__worktrees-gpx", Cwd: "/home/mara/src/kompass__worktrees/gpx", GitRoot: "/home/mara/src/kompass", Files: 1, Versions: 1, LastChange: at},
		{ID: 3, Harness: "claude", Key: r.alt, Files: 2, Versions: 2, LastChange: at},
		{ID: 6, Harness: "claude", Key: "-home-mara-ghost-dir", Files: 1, Versions: 1, LastChange: at},
		{ID: 4, Harness: "claude", Key: "-home-mara-src-leer"},
		{ID: 5, Harness: "claude", Key: "-home-mara-src-weg", Cwd: "/home/mara/src/weg", Deleted: 2, Versions: 4, LastChange: at},
	}, nil
}

// altKey is the key of a directory with hyphens in its names that exists
// below a temporary directory: ".../mein-projekt/web-app".
func altKey(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "mein-projekt", "web-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(dir, "-")
}

func realModel(t *testing.T, width int) Model {
	t.Helper()
	def, err := project.NewDefinition("kompass", "", "", []string{"~/src/kompass*"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := project.NewResolver([]project.Definition{def}, "/home/mara")
	if err != nil {
		t.Fatal(err)
	}
	var tm tea.Model = New(Options{Source: realSource{alt: altKey(t)}, Resolver: res, Home: "/home/mara"})
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: width, Height: 14})
	m, cmd := tm.(Model).Reload()
	return tuitest.Run(t, m, cmd).(Model)
}

// TestRealArchiveRows: rows are told apart by a readable path next to the
// name, the raw key never shows, a project without files is left out (and
// counted), and the columns line up.
func TestRealArchiveRows(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		m := realModel(t, width)
		out := ansi.Strip(m.View().Content)
		lines := strings.Split(out, "\n")
		if strings.Contains(out, "claude 0 files  ") {
			t.Errorf("width %d: empty row:\n%s", width, out)
		}
		var rows []string
		for _, l := range lines {
			if strings.Contains(l, "claude") {
				rows = append(rows, l)
			}
		}
		if len(rows) != 5 {
			t.Fatalf("width %d: %d rows, want 5 (the empty project is hidden):\n%s", width, len(rows), out)
		}
		at := func(r string) int { return ansi.StringWidth(r[:strings.Index(r, "claude")]) }
		col := at(rows[0])
		for _, r := range rows {
			if at(r) != col {
				t.Errorf("width %d: harness column not aligned:\n%s", width, out)
			}
		}
		for _, want := range []string{"~/src/kompass", "~/src/kompass__worktrees/gpx", "mein-projekt/web-app", "(path unknown)", "-home-mara-", "0 files, 2 deleted", "1 without files"} {
			if width >= 100 && !strings.Contains(out, want) {
				t.Errorf("width %d: lacks %q:\n%s", width, want, out)
			}
		}
		// Two rows carry the name kompass; their paths differ.
		if n := len(regexp.MustCompile(`kompass +~/`).FindAllString(out, -1)); width >= 100 && n < 2 {
			t.Errorf("width %d: kompass rows = %d:\n%s", width, n, out)
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Errorf("width %d: line of %d cells: %q", width, w, l)
			}
		}
	}
}
