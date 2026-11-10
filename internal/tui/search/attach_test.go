package search

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/attview"
	"github.com/seletz/total-recall/internal/tui/tuitest"
	"github.com/seletz/total-recall/internal/tui/viewer"
)

// attachSearcher answers every query with the same event hits and knows
// the attachments of some events.
type attachSearcher struct {
	hits []store.Hit
	atts map[string][]store.EventAttachment
	err  error // of EventsAttachments
}

func (s *attachSearcher) SearchTyped(_ context.Context, q store.TypedQuery) ([]store.TypeHits, error) {
	return []store.TypeHits{{Type: store.TypeEvent, Hits: s.hits, Total: len(s.hits)}}, nil
}

func (s *attachSearcher) EventsAttachments(_ context.Context, ids []string) (map[string][]store.EventAttachment, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := map[string][]store.EventAttachment{}
	for _, id := range ids {
		if a, ok := s.atts[id]; ok {
			out[id] = a
		}
	}
	return out, nil
}

func png(n int64) store.EventAttachment {
	return store.EventAttachment{BlobInfo: store.BlobInfo{SHA256: "ab", MediaType: "image/png", Bytes: n}}
}

func attachFixture() *attachSearcher {
	mk := func(id, text string) store.Hit {
		e := model.Event{ID: id, SessionID: "s", Actor: model.ActorHuman, Kind: model.KindMessage, Text: text, TS: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
		return store.Hit{Type: store.TypeEvent, TS: e.TS, Snippet: text, Event: &store.SearchHit{Event: e, SessionName: "sess"}}
	}
	return &attachSearcher{
		hits: []store.Hit{mk("e1", "look at this"), mk("e2", "nothing here")},
		atts: map[string][]store.EventAttachment{"e1": {png(2048), png(100)}},
	}
}

func driveAttach(t *testing.T, s *attachSearcher, w, h int) Model {
	t.Helper()
	m := New(Options{Searcher: s, Loader: &fakeLoader{}, Query: "x", Now: tuitest.Clock, Location: time.UTC})
	tm := tuitest.Drive(t, m, tea.WindowSizeMsg{Width: w, Height: h}, tuitest.Key("tab"))
	return tm.(Model)
}

func TestAttachmentMarkInList(t *testing.T) {
	m := driveAttach(t, attachFixture(), 100, 20)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	var e1, e2 string
	for _, l := range lines {
		left, _, _ := strings.Cut(l, "│")
		switch {
		case strings.Contains(left, "2026-10-01") && e1 == "":
			e1 = left
		case strings.Contains(left, "2026-10-01"):
			e2 = left
		}
	}
	if !strings.Contains(e1, viewer.AttachmentMark+"2") {
		t.Errorf("hit with two attachments: %q", e1)
	}
	if strings.Contains(e2, viewer.AttachmentMark) {
		t.Errorf("hit without attachments is marked: %q", e2)
	}
}

func TestAttachmentMarkNarrow(t *testing.T) {
	// The mark stays when the name is cut: it takes its room first.
	for _, w := range []int{80, 50, 44} {
		m := driveAttach(t, attachFixture(), w, 12)
		left, _, _ := strings.Cut(ansi.Strip(m.View().Content), "│")
		if !strings.Contains(left, viewer.AttachmentMark) {
			t.Errorf("w=%d: mark lost:\n%s", w, left)
		}
	}
}

func TestAttachmentMarkWithoutSource(t *testing.T) {
	// A searcher that cannot tell leaves the list as it was.
	type plain struct{ Searcher }
	s := attachFixture()
	m := New(Options{Searcher: plain{s}, Loader: &fakeLoader{}, Query: "x", Now: tuitest.Clock, Location: time.UTC})
	tm := tuitest.Drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	if strings.Contains(ansi.Strip(tm.(Model).View().Content), viewer.AttachmentMark) {
		t.Error("mark without a source")
	}
}

func TestAttachmentMarkSourceFails(t *testing.T) {
	s := attachFixture()
	s.err = errors.New("db gone")
	m := driveAttach(t, s, 100, 20)
	if got := ansi.Strip(m.View().Content); strings.Contains(got, viewer.AttachmentMark) || !strings.Contains(got, "look at this") {
		t.Errorf("a failing lookup only drops the marks:\n%s", got)
	}
}

func TestAttachKey(t *testing.T) {
	m := driveAttach(t, attachFixture(), 100, 20)
	_, cmd := m.Update(tuitest.Key("v"))
	if cmd == nil {
		t.Fatal("v on a hit with attachments returns a command")
	}
	open, ok := cmd().(attview.Open)
	if !ok || open.SessionID != "s" || open.EventID != "e1" || open.Title != "sess" {
		t.Errorf("v sends %+v", open)
	}
	// On the hit without attachments, v does nothing.
	tm := tuitest.Send(t, m, tuitest.Key("j"))
	if _, cmd := tm.Update(tuitest.Key("v")); cmd != nil {
		t.Errorf("v on a hit without attachments: %v", cmd())
	}
}

func TestAttachKeyInInputTypes(t *testing.T) {
	m := New(Options{Searcher: attachFixture(), Query: "x", Now: tuitest.Clock, Location: time.UTC})
	tm := tuitest.Drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 20}, tuitest.Key("v"))
	if got := tm.(Model).input.Value(); got != "xv" {
		t.Errorf("input = %q, want the key typed", got)
	}
}

func TestAttachHelp(t *testing.T) {
	m := driveAttach(t, attachFixture(), 100, 20)
	has := func(m Model) bool {
		for _, b := range m.ShortHelp() {
			if b.Help().Desc == "view attachments" {
				return true
			}
		}
		return false
	}
	if !has(m) {
		t.Error("footer names the key on a hit with attachments")
	}
	if tm := tuitest.Send(t, m, tuitest.Key("j")); has(tm.(Model)) {
		t.Error("footer names the key on a hit without attachments")
	}
}

func TestPreviewListsAttachments(t *testing.T) {
	s := attachFixture()
	ld := &attachLoader{fakeLoader: &fakeLoader{events: []model.Event{s.hits[0].Event.Event}}, src: s}
	v, err := loadViewer(context.Background(), ld, s.hits[0], "")
	if err != nil {
		t.Fatal(err)
	}
	out := ansi.Strip(strings.Join(v.Render(viewer.Env{}, 60).Body, "\n"))
	if !strings.Contains(out, "▣ image/png 2.0 KiB") || !strings.Contains(out, "▣ image/png 100 B") {
		t.Errorf("preview body:\n%s", out)
	}
}

// attachLoader is a Loader that also knows the attachments.
type attachLoader struct {
	*fakeLoader
	src *attachSearcher
}

func (l *attachLoader) EventsAttachments(ctx context.Context, ids []string) (map[string][]store.EventAttachment, error) {
	return l.src.EventsAttachments(ctx, ids)
}
