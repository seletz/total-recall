package search

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// fakeLoader serves one session whose events are given; it records the
// contexts of its calls so a test can tell which load was cancelled.
type fakeLoader struct {
	mu     sync.Mutex
	events []model.Event
	ctxs   []context.Context
}

func (f *fakeLoader) FindSession(ctx context.Context, ref string) (store.SessionInfo, error) {
	f.mu.Lock()
	f.ctxs = append(f.ctxs, ctx)
	f.mu.Unlock()
	if ref != "s" {
		return store.SessionInfo{}, errors.New("not found")
	}
	s := store.SessionInfo{}
	s.ID = "s"
	return s, nil
}

func (f *fakeLoader) Events(context.Context, string, bool) ([]model.Event, error) {
	return f.events, nil
}

func ev(id string, actor model.Actor) model.Event {
	kind := model.KindMessage
	if actor == model.ActorTool {
		kind = model.KindToolResult
	}
	return model.Event{ID: id, Actor: actor, Kind: kind}
}

// TestWindow checks the exchange shown around a hit: the human (or peer)
// prompt before it and the agent reply after it, with the hit itself at
// either end of the window when it is the first or the last event or when
// no reply follows.
func TestWindow(t *testing.T) {
	events := []model.Event{
		ev("h1", model.ActorHuman), ev("a1", model.ActorAgent),
		ev("h2", model.ActorHuman), ev("t1", model.ActorAgent), ev("r1", model.ActorTool), ev("a2", model.ActorAgent),
		ev("p1", model.ActorPeer), ev("a3", model.ActorAgent),
		ev("h3", model.ActorHuman),
	}
	events[3].Kind = model.KindToolUse
	tests := []struct {
		name      string
		hit       string
		wantStart int
		wantEnd   int
	}{
		{"first event, human hit", "h1", 0, 1},
		{"agent hit takes the prompt before it", "a1", 0, 1},
		{"human hit in the middle", "h2", 2, 5},
		{"tool use between prompt and reply", "t1", 2, 5},
		{"tool result between prompt and reply", "r1", 2, 5},
		{"agent reply after tools", "a2", 2, 5},
		{"peer counts as prompt", "p1", 6, 7},
		{"reply to a peer", "a3", 6, 7},
		{"last event without reply", "h3", 8, 8},
		{"unknown hit", "zz", -1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := window(events, tt.hit)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("window(%q) = %d, %d; want %d, %d", tt.hit, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
	t.Run("agent without prompt", func(t *testing.T) {
		start, end := window([]model.Event{ev("a1", model.ActorAgent), ev("h1", model.ActorHuman)}, "a1")
		if start != 0 || end != 0 {
			t.Errorf("window = %d, %d; want 0, 0", start, end)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if start, end := window(nil, "h1"); start != -1 || end != -1 {
			t.Errorf("window = %d, %d; want -1, -1", start, end)
		}
	})
}

// TestTerms checks the words handed to the highlighter: the query's words,
// and in raw mode without FTS5 operators, quotes and prefix stars.
func TestTerms(t *testing.T) {
	tests := []struct {
		name  string
		query string
		raw   bool
		want  []string
	}{
		{"plain", "release notes", false, []string{"release", "notes"}},
		{"whitespace", "  release \t notes ", false, []string{"release", "notes"}},
		{"empty", "   ", false, nil},
		{"plain keeps quotes", `"release"`, false, []string{`"release"`}},
		{"raw strips operators", `release AND NOT notes OR tag`, true, []string{"release", "notes", "tag"}},
		{"raw strips quotes and stars", `"beta release" tag*`, true, []string{"beta", "release", "tag"}},
		{"raw near", `NEAR(release tag, 5)`, true, []string{"release", "tag", "5"}},
		{"raw only operators", `AND OR`, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terms(tt.query, tt.raw); !slices.Equal(got, tt.want) {
				t.Errorf("terms(%q, %v) = %q, want %q", tt.query, tt.raw, got, tt.want)
			}
		})
	}
}

// TestFilterLabel checks the header text of every filter state.
func TestFilterLabel(t *testing.T) {
	tests := []struct {
		name string
		f    filters
		want string
	}{
		{"default", filters{}, "actor: all  since: all  repo: *"},
		{"actor", filters{actor: 1}, "actor: human  since: all  repo: *"},
		{"span", filters{span: 2}, "actor: all  since: 30d  repo: *"},
		{"repo", filters{repo: "beta"}, "actor: all  since: all  repo: beta"},
		{"raw", filters{raw: true}, "actor: all  since: all  repo: *  raw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.label(); got != tt.want {
				t.Errorf("label() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPreviewScroll checks that an exchange longer than the column is cut
// so the hit stays visible: the head keeps its rows, and the hit's
// separator and text are within the column.
func TestPreviewScroll(t *testing.T) {
	long := strings.Repeat("line\n", 40)
	hit := ev("a1", model.ActorAgent)
	hit.Text = "the reply"
	prompt := ev("h1", model.ActorHuman)
	prompt.Text = long
	m := New(Options{Searcher: &fakeSearcher{}, Loader: &fakeLoader{}, Location: time.UTC})
	m.hits = []store.SearchHit{{Event: hit}}
	m.query = "reply"
	m.preview = preview{eventID: "a1", events: []model.Event{prompt, hit}, hit: 1}
	tests := []struct {
		name string
		rows int
	}{
		{"short column", 12},
		{"tall column", 30},
		{"everything fits", 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ansi.Strip(m.previewView(40, tt.rows))
			lines := strings.Split(out, "\n")
			if len(lines) > tt.rows {
				t.Errorf("preview has %d lines, want at most %d", len(lines), tt.rows)
			}
			if !strings.Contains(lines[0], "(untitled)") || !strings.HasPrefix(lines[3], "───") {
				t.Errorf("head lost:\n%s", out)
			}
			at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "agent ◄") })
			if at < 4 || at >= tt.rows-1 {
				t.Errorf("hit separator at line %d of %d rows:\n%s", at, tt.rows, out)
			}
			if !strings.Contains(out, "the reply") {
				t.Errorf("hit text missing:\n%s", out)
			}
		})
	}
}

// TestStalePreviewDiscarded is the sequence logic of the preview: moving
// the mark cancels the load of the previous hit, and its answer arriving
// late must not replace the newer hit's exchange.
func TestStalePreviewDiscarded(t *testing.T) {
	h1, h2 := ev("h1", model.ActorHuman), ev("h2", model.ActorHuman)
	h1.SessionID, h2.SessionID = "s", "s"
	h1.Text, h2.Text = "first", "second"
	ld := &fakeLoader{events: []model.Event{h1, ev("a1", model.ActorAgent), h2}}
	m := New(Options{Searcher: &fakeSearcher{}, Loader: ld, Now: tuitest.Clock})
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	nm, cmd := m.Update(resultMsg{seq: m.seq, query: "x", hits: []store.SearchHit{{Event: h1}, {Event: h2}}})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("result scheduled no preview")
	}
	nm, load1 := m.Update(previewTickMsg{seq: m.preview.seq})
	m = nm.(Model)
	if load1 == nil {
		t.Fatal("tick started no load")
	}
	m, _ = press(m, "tab")
	m, cmd = press(m, "j")
	if cmd == nil {
		t.Fatal("moving scheduled no preview")
	}
	nm, load2 := m.Update(previewTickMsg{seq: m.preview.seq})
	m = nm.(Model)
	if load2 == nil {
		t.Fatal("second tick started no load")
	}

	// The second load answers first, the first one later.
	m = update(t, m, load2())
	if m.preview.eventID != "h2" || len(m.preview.events) != 1 || m.preview.events[0].Text != "second" {
		t.Fatalf("after fresh result: preview = %+v", m.preview)
	}
	m = update(t, m, load1())
	if m.preview.eventID != "h2" {
		t.Errorf("after stale result: preview shows %q, want h2", m.preview.eventID)
	}
	if !m.previewReady() {
		t.Error("preview not ready after its result")
	}

	ld.mu.Lock()
	defer ld.mu.Unlock()
	if len(ld.ctxs) != 2 {
		t.Fatalf("loader called %d times, want 2", len(ld.ctxs))
	}
	if ld.ctxs[0].Err() == nil {
		t.Error("first load's context not cancelled by the move")
	}

	// Back on the loaded hit no new load is scheduled; a stale tick is
	// ignored.
	m, _ = press(m, "k")
	if _, cmd := m.Update(previewTickMsg{seq: m.preview.seq - 1}); cmd != nil {
		t.Error("stale tick started a load")
	}
	m, _ = press(m, "j")
	if m.preview.eventID != "h2" || !m.previewReady() {
		t.Error("returning to the loaded hit lost the preview")
	}
}
