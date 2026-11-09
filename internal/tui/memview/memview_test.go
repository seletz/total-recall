package memview

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// kompass is the demo project with several versions and a deletion.
func kompass(t *testing.T, st *store.Store) store.MemoryProject {
	t.Helper()
	ps, err := st.MemoryProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if strings.HasSuffix(p.Key, "-kompass") {
			return p
		}
	}
	t.Fatal("no kompass memory project in the demo archive")
	return store.MemoryProject{}
}

// opened is the view over the demo archive, sized 120×30, with the memory
// of kompass open and path marked.
func opened(t *testing.T, path string) Model {
	t.Helper()
	st, _ := fixturedb.Demo(t)
	m := New(st, demo.Location, nil)
	var tm tea.Model = m
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 120, Height: 30})
	tm = tuitest.Send(t, tm, Open{ProjectID: kompass(t, st).ID, Project: "kompass", Harness: "claude", Path: path})
	return tm.(Model)
}

func drive(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	var tm tea.Model = m
	for _, msg := range msgs {
		tm = tuitest.Send(t, tm, msg)
	}
	return tm.(Model)
}

func paths(files []store.MemoryFile) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func TestFilesPage(t *testing.T) {
	m := opened(t, "release-process.md")
	if got, want := strings.Join(paths(m.files), " "), "MEMORY.md conventions.md release-process.md old-notes.md"; got != want {
		t.Errorf("order = %q, want %q (index first, deleted last)", got, want)
	}
	if f := m.marked(); f == nil || f.Path != "release-process.md" {
		t.Fatalf("marked = %+v, want release-process.md", f)
	}
	frame := tuitest.Frame(m)
	for _, want := range []string{"kompass", "memory", "MEMORY.md", "✗ old-notes.md", "deleted", "goreleaser", "3 files, 1 deleted"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q:\n%s", want, frame)
		}
	}
}

func TestMarkWithoutPath(t *testing.T) {
	m := opened(t, "")
	if f := m.marked(); f == nil || f.Path != IndexFile {
		t.Errorf("marked = %+v, want the index", f)
	}
}

func TestHistory(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		keys  []string
		marks int    // versions listed
		show  string // in the frame
		not   string // not in the frame
	}{
		{"current version", "release-process.md", []string{"enter"}, 3, "version 3 of 3", "version 2 of 3"},
		{"earlier version", "release-process.md", []string{"enter", "j"}, 3, "version 2 of 3", "Why:"},
		{"first version", "release-process.md", []string{"enter", "j", "j"}, 3, "A release is a tag on", "goreleaser"},
		{"deleted file", "old-notes.md", []string{"enter"}, 2, "The file was deleted at this point.", "Scratch notes"},
		{"before the deletion", "old-notes.md", []string{"enter", "j"}, 2, "compact binary", "deleted at this point"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, opened(t, tt.path), tuitest.Keys(tt.keys...)...)
			if m.page != pageHistory || len(m.versions) != tt.marks {
				t.Fatalf("page %d, %d versions, want history with %d", m.page, len(m.versions), tt.marks)
			}
			frame := tuitest.Frame(m)
			if !strings.Contains(frame, tt.show) {
				t.Errorf("frame lacks %q:\n%s", tt.show, frame)
			}
			if tt.not != "" && strings.Contains(frame, tt.not) {
				t.Errorf("frame has %q:\n%s", tt.not, frame)
			}
		})
	}
}

func TestVersionsNewestFirst(t *testing.T) {
	m := drive(t, opened(t, "old-notes.md"), tuitest.Keys("enter")...)
	if !m.versions[0].Deleted || m.versions[1].Deleted || !m.versions[0].TS.After(m.versions[1].TS) {
		t.Errorf("versions = %+v, want the deletion first, then the content", m.versions)
	}
}

// TestBack: esc goes from the history to the files, from there out.
func TestBack(t *testing.T) {
	m := drive(t, opened(t, "release-process.md"), tuitest.Keys("enter")...)
	m = drive(t, m, tuitest.Key("esc"))
	if m.page != pageFiles || m.marked().Path != "release-process.md" {
		t.Fatalf("page %d marked %v, want the files page on the same file", m.page, m.marked())
	}
	_, cmd := m.Update(tuitest.Key("esc"))
	if cmd == nil {
		t.Fatal("esc on the files page does nothing")
	}
	if _, ok := cmd().(Back); !ok {
		t.Errorf("esc sent %T, want Back", cmd())
	}
}

func TestMouse(t *testing.T) {
	m := opened(t, "MEMORY.md")
	row := func(i int) tea.MouseClickMsg {
		return tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: chromeRows - 1 + i}
	}
	// A click marks, a click on the marked file opens the history.
	m = drive(t, m, row(2))
	if m.marked().Path != "release-process.md" || m.page != pageFiles {
		t.Fatalf("click: marked %v page %d", m.marked(), m.page)
	}
	m = drive(t, m, row(2))
	if m.page != pageHistory {
		t.Fatalf("second click: page %d, want the history", m.page)
	}
	// On the history page a click marks a version.
	m = drive(t, m, row(1))
	if m.vcursor != 1 {
		t.Errorf("click in the history: version %d marked, want 1", m.vcursor)
	}
	// The wheel over the list moves the mark, over the content scrolls it.
	m = drive(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 3, Y: 5})
	if m.vcursor != 0 {
		t.Errorf("wheel over the list: version %d, want 0", m.vcursor)
	}
	// A click on the content or the status line does nothing.
	if m2 := drive(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 100, Y: 6}); m2.vcursor != m.vcursor || m2.page != m.page {
		t.Error("click on the content changed the view")
	}
}

func TestScroll(t *testing.T) {
	m := opened(t, "release-process.md")
	m.height = 12 // shorter than the file, so the content scrolls
	if m.scroll != -1 {
		t.Fatalf("scroll = %d before scrolling, want automatic", m.scroll)
	}
	m = drive(t, m, tuitest.Key("f"))
	if m.scroll <= 0 {
		t.Errorf("page down: scroll = %d, want > 0", m.scroll)
	}
	m = drive(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 100, Y: 5})
	before := m.scroll
	m = drive(t, m, tuitest.Key("b"), tuitest.Key("b"))
	if m.scroll != 0 || before < 0 {
		t.Errorf("page up: scroll = %d, want the top", m.scroll)
	}
	// A new mark starts at the automatic position again.
	if m = drive(t, m, tuitest.Key("j")); m.scroll != -1 {
		t.Errorf("after moving: scroll = %d, want automatic", m.scroll)
	}
}

func TestConfiguredKeys(t *testing.T) {
	km, err := keys.Resolve(map[string][]string{keys.MemoryDown: {"n"}, keys.MemoryOpen: {"o"}})
	if err != nil {
		t.Fatal(err)
	}
	m := opened(t, "MEMORY.md")
	m = m.SetKeys(km).(Model)
	m = drive(t, m, tuitest.Key("n"))
	if m.cursor != 1 {
		t.Errorf("n moves to %d, want 1", m.cursor)
	}
	m = drive(t, m, tuitest.Key("j"))
	if m.cursor != 1 {
		t.Errorf("j (rebound) moves to %d, want to stay at 1", m.cursor)
	}
	if m = drive(t, m, tuitest.Key("o")); m.page != pageHistory {
		t.Error("o does not open the history")
	}
}

func TestReloadKeepsMarks(t *testing.T) {
	m := drive(t, opened(t, "release-process.md"), tuitest.Keys("enter", "j")...)
	tm, cmd := m.Reload()
	m = tuitest.Run(t, tm, cmd).(Model)
	if m.page != pageHistory || m.histPath != "release-process.md" || m.vcursor != 1 {
		t.Errorf("after reload: page %d path %q version %d, want history of release-process.md, version 1", m.page, m.histPath, m.vcursor)
	}
	if m = drive(t, m, tuitest.Key("esc")); m.marked().Path != "release-process.md" {
		t.Errorf("file mark after reload: %v", m.marked())
	}
}

func TestNoDatabase(t *testing.T) {
	var tm tea.Model = New(nil, nil, nil)
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 80, Height: 20})
	tm = tuitest.Send(t, tm, Open{ProjectID: 1, Project: "x"})
	if f := tuitest.Frame(tm); !strings.Contains(f, "no database") {
		t.Errorf("frame lacks the hint:\n%s", f)
	}
}

func TestSmallWindow(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 0, Height: 0}, {Width: 1, Height: 1}, {Width: 20, Height: 4}, {Width: 80, Height: 24}} {
		m := opened(t, "release-process.md")
		m = drive(t, m, size, tuitest.Key("enter"), tuitest.Key("j"), tuitest.Key("f"))
		_ = m.View() // must not panic
	}
}
