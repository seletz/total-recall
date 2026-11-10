package session

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

var tinyPNG = []byte("\x89PNG\r\n\x1a\nnot really a picture")

// attachedStore ingests one synthetic session: a prompt, a prompt that
// carries an image and a document, a reply.
func attachedStore(t *testing.T, withAttachments bool) *store.Store {
	t.Helper()
	dir := t.TempDir()
	const sid, cwd = "11111111-0000-4000-8000-000000000001", "/home/dev/app"
	lines := []testfixture.Line{
		testfixture.CustomTitle(sid, "With pictures"),
		testfixture.User(sid, "u1", "2026-10-01T09:00:00.000Z", cwd, "first prompt"),
		testfixture.Assistant(sid, "a1", "2026-10-01T09:00:10.000Z", cwd, "first reply"),
	}
	if withAttachments {
		lines = append(lines, testfixture.UserWithAttachments(sid, "u2", "2026-10-01T09:01:00.000Z", cwd, "see the picture",
			testfixture.Attached{Kind: "image", MediaType: "image/png", Data: tinyPNG},
			testfixture.Attached{Kind: "document", MediaType: "application/pdf", Data: []byte("%PDF-1.4\n")}))
	}
	lines = append(lines, testfixture.Assistant(sid, "a2", "2026-10-01T09:01:20.000Z", cwd, "second reply"))
	testfixture.WriteFile(t, dir, filepath.Join("projects", "-home-dev-app", sid+".jsonl"), testfixture.Join(lines...))
	st, _ := fixturedb.Ingest(t, filepath.Join(dir, "projects"), nil)
	return st
}

func openAttached(t *testing.T, st *store.Store) Model { return openAttachedAt(t, st, 30) }

// openAttachedAt is openAttached in a window of the given height.
func openAttachedAt(t *testing.T, st *store.Store, height int) Model {
	t.Helper()
	return tuitest.Drive(t, New(st, testfixture.ActivityLocation, nil),
		tea.WindowSizeMsg{Width: 100, Height: height}, Open{SessionID: "claude:11111111-0000-4000-8000-000000000001"}).(Model)
}

func TestAttachmentMarkOnSeparator(t *testing.T) {
	m := openAttached(t, attachedStore(t, true))
	var marked []string
	for _, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(l, "──") && strings.Contains(l, "▣") {
			marked = append(marked, l)
		}
	}
	// The image and the document sit on events of their own: two marks.
	if len(marked) != 2 {
		t.Fatalf("separators with the mark = %d, want 2:\n%s", len(marked), ansi.Strip(m.View().Content))
	}
	for _, l := range marked {
		if !strings.Contains(l, "▣1") {
			t.Errorf("separator lacks the count: %q", l)
		}
	}
}

func TestNoMarkWithoutAttachments(t *testing.T) {
	m := openAttached(t, attachedStore(t, false))
	if got := ansi.Strip(m.View().Content); strings.Contains(got, "▣") {
		t.Errorf("mark in a session without attachments:\n%s", got)
	}
}

func TestAttachKey(t *testing.T) {
	m := openAttached(t, attachedStore(t, true))
	_, cmd := m.Update(tuitest.Key("v"))
	if cmd == nil {
		t.Fatal("v returns a command")
	}
	open, ok := cmd().(OpenAttachments)
	if !ok || open.SessionID != m.session.ID || open.EventID == "" || open.Title != "With pictures" {
		t.Errorf("v sends %+v", open)
	}
	// The view opens on an event that has attachments.
	if n := len(m.attached[open.EventID]); n == 0 {
		t.Errorf("event %s has no attachments", open.EventID)
	}
}

func TestAttachKeyPicksNearTheTop(t *testing.T) {
	// A small window, so the transcript scrolls.
	m := openAttachedAt(t, attachedStore(t, true), 7)
	first, second := m.attachedEvents[0], m.attachedEvents[1]
	tests := []struct {
		name string
		line int // first visible line
		want string
	}{
		{"above both", 0, first},
		{"at the first", m.eventAt[m.indexOf(first)], first},
		{"inside the first", m.eventAt[m.indexOf(first)] + 1, first},
		{"at the second", m.eventAt[m.indexOf(second)], second},
		{"past both takes the last", m.eventAt[len(m.eventAt)-1], second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := m
			mm.vp.SetYOffset(tt.line)
			mm.current = -1
			_, cmd := mm.Update(tuitest.Key("v"))
			if cmd == nil {
				t.Fatal("no command")
			}
			if got := cmd().(OpenAttachments).EventID; got != tt.want {
				t.Errorf("event = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAttachKeyWithoutAttachments(t *testing.T) {
	m := openAttached(t, attachedStore(t, false))
	next, cmd := m.Update(tuitest.Key("v"))
	if cmd != nil {
		t.Errorf("v without attachments sends %v", cmd())
	}
	if got := ansi.Strip(next.(Model).View().Content); !strings.Contains(got, "no attachments") {
		t.Errorf("no notice:\n%s", got)
	}
}

func TestAttachHelp(t *testing.T) {
	has := func(m Model) bool {
		for _, b := range m.ShortHelp() {
			if b.Help().Desc == "view attachments" {
				return true
			}
		}
		return false
	}
	if !has(openAttached(t, attachedStore(t, true))) {
		t.Error("footer lacks the attachment key in a session with attachments")
	}
	if has(openAttached(t, attachedStore(t, false))) {
		t.Error("footer names the attachment key in a session without")
	}
}
