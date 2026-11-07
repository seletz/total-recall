package memview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

func ver(seq int64, day int, sha string, bytes int64) store.MemoryVersion {
	return store.MemoryVersion{Seq: seq, TS: time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC), SHA256: sha, Bytes: bytes, Deleted: sha == ""}
}

func TestTimeline(t *testing.T) {
	tests := []struct {
		name string
		in   map[string][]store.MemoryVersion
		want []string // path:kind:delta, newest first
	}{
		{"none", nil, nil},
		{"added", map[string][]store.MemoryVersion{"a.md": {ver(1, 1, "x", 10)}}, []string{"a.md:added:10"}},
		{"changed", map[string][]store.MemoryVersion{"a.md": {ver(1, 1, "x", 10), ver(2, 2, "y", 25)}}, []string{"a.md:changed:15", "a.md:added:10"}},
		{"shrunk", map[string][]store.MemoryVersion{"a.md": {ver(1, 1, "x", 30), ver(2, 2, "y", 5)}}, []string{"a.md:changed:-25", "a.md:added:30"}},
		{"deleted and back", map[string][]store.MemoryVersion{"a.md": {ver(1, 1, "x", 10), ver(2, 2, "", 0), ver(3, 3, "y", 12)}},
			[]string{"a.md:added:12", "a.md:deleted:-10", "a.md:added:10"}},
		{"files interleave", map[string][]store.MemoryVersion{
			"a.md": {ver(1, 1, "x", 10), ver(4, 5, "y", 11)},
			"b.md": {ver(2, 3, "z", 7), ver(3, 4, "", 0)},
		}, []string{"a.md:changed:1", "b.md:deleted:-7", "b.md:added:7", "a.md:added:10"}},
		{"same time: higher sequence first", map[string][]store.MemoryVersion{
			"a.md": {ver(1, 1, "x", 1)}, "b.md": {ver(2, 1, "y", 2)},
		}, []string{"b.md:added:2", "a.md:added:1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, e := range Timeline(tt.in) {
				got = append(got, e.Path+":"+e.Kind.String()+":"+itoa(e.Delta))
			}
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("Timeline = %v, want %v", got, tt.want)
			}
		})
	}
}

func itoa(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := ""
	for {
		s = string(rune('0'+n%10)) + s
		if n /= 10; n == 0 {
			break
		}
	}
	if neg {
		return "-" + s
	}
	return s
}

func TestDiffMode(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		show []string
		not  []string
	}{
		{"against the version before", []string{"enter", "d"},
			[]string{"history of release-process.md", "diff", "version 2 of 3 → ", "version 3 of 3", "Why:", "published"}, nil},
		{"first version is all new", []string{"enter", "j", "j", "d"},
			[]string{"(no earlier version)", "+ A release is a tag on", "-0"}, []string{"- A release"}},
		{"base", []string{"enter", "j", "j", "c", "k", "k", "d"},
			[]string{"version 1 of 3 (base)", "base", "compared with the base", "- A release is a tag on", "+ A release is a signed tag"}, nil},
		{"base dropped by a second c", []string{"enter", "j", "j", "c", "c", "k", "d"},
			[]string{"version 1 of 3 → ", "version 2 of 3"}, []string{"(base)", "base"}},
		{"base on the marked version compares with the one before", []string{"enter", "c", "d"},
			[]string{"version 2 of 3 → "}, []string{"(base)"}},
		{"diff off again", []string{"enter", "d", "d"}, []string{"version 3 of 3"}, []string{"→"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, opened(t, "release-process.md"), tuitest.Keys(tt.keys...)...)
			frame := tuitest.Frame(m)
			for _, w := range tt.show {
				if !strings.Contains(frame, w) {
					t.Errorf("frame lacks %q:\n%s", w, frame)
				}
			}
			for _, w := range tt.not {
				if strings.Contains(frame, w) {
					t.Errorf("frame has %q:\n%s", w, frame)
				}
			}
		})
	}
}

// TestDiffOfDeleted: a deletion has no diff, the version after one is all
// new.
func TestDiffOfDeleted(t *testing.T) {
	m := drive(t, opened(t, "old-notes.md"), tuitest.Keys("enter", "d")...)
	if f := tuitest.Frame(m); !strings.Contains(f, "The file was deleted at this point.") {
		t.Errorf("deleted version in diff mode:\n%s", f)
	}
	m = drive(t, m, tuitest.Key("j"))
	if f := tuitest.Frame(m); !strings.Contains(f, "(no earlier version)") || !strings.Contains(f, "+ Routes were stored") {
		t.Errorf("first version after the deletion:\n%s", f)
	}
}

func TestTimelinePage(t *testing.T) {
	m := drive(t, opened(t, "release-process.md"), tuitest.Key("t"))
	if m.page != pageTimeline || len(m.events) != 10 {
		t.Fatalf("page %d with %d events, want the timeline with the 10 changes of the demo project", m.page, len(m.events))
	}
	// The newest change first: the index of 2026-10-31 22:40.
	if e := m.events[0]; e.Path != "MEMORY.md" || e.Kind != Changed {
		t.Errorf("first event = %s %s", e.Path, e.Kind)
	}
	frame := tuitest.Frame(m)
	for _, want := range []string{"kompass  timeline", "2026-10-31 22:40 ~ MEMORY.md", "2026-10-28 10:00 ✗ old-notes.md", "2026-10-15 17:55 + old-notes.md", "10 changes", "@@"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q:\n%s", want, frame)
		}
	}
}

func TestTimelineKinds(t *testing.T) {
	m := drive(t, opened(t, ""), tuitest.Key("t"))
	find := func(path string, kind EventKind) int {
		for i, e := range m.events {
			if e.Path == path && e.Kind == kind {
				return i
			}
		}
		t.Fatalf("no %s event for %s", kind, path)
		return -1
	}
	tests := []struct {
		name string
		path string
		kind EventKind
		want string
	}{
		{"deleted", "old-notes.md", Deleted, "The file was deleted."},
		{"added shows the content", "old-notes.md", Added, "Routes were stored"},
		{"changed shows the diff", "release-process.md", Changed, "@@"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := m
			m.tcursor = find(tt.path, tt.kind)
			cmd := m.ensureText() // what moving the mark does
			if f := tuitest.Frame(tuitest.Run(t, m, cmd)); !strings.Contains(f, tt.want) {
				t.Errorf("frame lacks %q:\n%s", tt.want, f)
			}
		})
	}
}

// TestTimelineNavigation: enter opens the history of the change on its
// version, esc goes back to the timeline, then to the files, then out.
func TestTimelineNavigation(t *testing.T) {
	m := drive(t, opened(t, ""), tuitest.Key("t"))
	for i, e := range m.events {
		if e.Path == "release-process.md" && e.Kind == Changed {
			down := make([]string, i)
			for j := range down {
				down[j] = "j"
			}
			m = drive(t, m, tuitest.Keys(down...)...)
			break
		}
	}
	want := m.markedEvent()
	if want == nil || want.Path != "release-process.md" {
		t.Fatalf("marked %+v, want a release-process.md change", want)
	}
	m = drive(t, m, tuitest.Key("enter"))
	if m.page != pageHistory || m.histPath != "release-process.md" || m.markedVersion().Seq != want.Version.Seq {
		t.Fatalf("history page %d of %q on version %v, want release-process.md on seq %d", m.page, m.histPath, m.markedVersion(), want.Version.Seq)
	}
	m = drive(t, m, tuitest.Key("esc"))
	if m.page != pageTimeline {
		t.Fatalf("esc from the history opened page %d, want the timeline", m.page)
	}
	m = drive(t, m, tuitest.Key("esc"))
	if m.page != pageFiles {
		t.Fatalf("esc from the timeline opened page %d, want the files", m.page)
	}
	// From the files page the history returns there.
	m = drive(t, m, tuitest.Keys("enter", "esc")...)
	if m.page != pageFiles {
		t.Errorf("history opened from the files returned to page %d", m.page)
	}
}

func TestTimelineMouse(t *testing.T) {
	m := drive(t, opened(t, ""), tuitest.Key("t"))
	click := func(i int) tea.MouseClickMsg {
		return tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: chromeRows - 1 + i}
	}
	m = drive(t, m, click(3))
	if m.tcursor != 3 || m.page != pageTimeline {
		t.Fatalf("click: change %d, page %d", m.tcursor, m.page)
	}
	m = drive(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 3, Y: 5})
	if m.tcursor != 4 {
		t.Errorf("wheel: change %d, want 4", m.tcursor)
	}
	m = drive(t, m, click(4))
	if m.page != pageHistory {
		t.Errorf("second click: page %d, want the history", m.page)
	}
}

func TestMemoryKeysConfigured(t *testing.T) {
	km, err := keys.Resolve(map[string][]string{keys.MemoryDiff: {"x"}, keys.MemoryTimeline: {"w"}, keys.MemoryBase: {"v"}})
	if err != nil {
		t.Fatal(err)
	}
	m := opened(t, "release-process.md").SetKeys(km).(Model)
	m = drive(t, m, tuitest.Keys("enter", "x")...)
	if !m.diff {
		t.Error("x does not switch the diff on")
	}
	m = drive(t, m, tuitest.Key("d"))
	if !m.diff {
		t.Error("d (no longer bound) switched the diff")
	}
	m = drive(t, m, tuitest.Keys("v")...)
	if m.base == 0 {
		t.Error("v does not mark the base")
	}
	if m = drive(t, m, tuitest.Key("w")); m.page != pageTimeline {
		t.Error("w does not open the timeline")
	}
}

func TestReloadTimeline(t *testing.T) {
	m := drive(t, opened(t, ""), tuitest.Keys("t", "j", "j")...)
	want := m.markedEvent().Version.Seq
	tm, cmd := m.Reload()
	m = tuitest.Run(t, tm, cmd).(Model)
	if m.page != pageTimeline || m.markedEvent() == nil || m.markedEvent().Version.Seq != want {
		t.Errorf("after reload: page %d marked %+v, want the timeline on seq %d", m.page, m.markedEvent(), want)
	}
}

func TestHelpCovers(t *testing.T) {
	var got []string
	for _, col := range newKeyMap(nil).FullHelp() {
		for _, b := range col {
			got = append(got, b.Help().Desc)
		}
	}
	for _, want := range []string{"diff on/off", "compare with this version", "timeline", "history of file"} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("help lacks %q: %v", want, got)
		}
	}
}
