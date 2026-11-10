package search

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/tuitest"
	"github.com/seletz/total-recall/internal/tui/viewer"
)

// recorder answers every query with one event hit that carries an
// attachment of its own (SearchHit.Attachments, as the store fills it for
// a filtered search) and remembers the queries.
type recorder struct {
	mu      sync.Mutex
	queries []store.TypedQuery
}

func (r *recorder) SearchTyped(_ context.Context, q store.TypedQuery) ([]store.TypeHits, error) {
	r.mu.Lock()
	r.queries = append(r.queries, q)
	r.mu.Unlock()
	e := model.Event{ID: "e1", SessionID: "s", Actor: model.ActorTool, Kind: model.KindToolResult, Text: "[image image/png 2.0 KiB]", TS: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	hit := store.Hit{Type: store.TypeEvent, TS: e.TS, Snippet: e.Text, Event: &store.SearchHit{Event: e, SessionName: "shots",
		Attachments: []store.EventAttachment{{BlobInfo: store.BlobInfo{SHA256: "ab", MediaType: "image/png", Bytes: 2048}, EventID: "e1"}}}}
	var out []store.TypeHits
	for _, t := range q.Types {
		g := store.TypeHits{Type: t, Hits: []store.Hit{}}
		if t == store.TypeEvent && (strings.TrimSpace(q.Text) != "" || q.Attachment != nil) {
			g.Hits, g.Total = []store.Hit{hit}, 1
		}
		out = append(out, g)
	}
	return out, nil
}

func (r *recorder) last() store.TypedQuery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queries[len(r.queries)-1]
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries)
}

func newFilterModel(t *testing.T, s Searcher, query string) tea.Model {
	t.Helper()
	m := New(Options{Searcher: s, Query: query, Now: tuitest.Clock, Location: time.UTC})
	return tuitest.Drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 20}, tuitest.Key("tab"))
}

func frameOf(m tea.Model) string { return ansi.Strip(m.(Model).View().Content) }

func TestAttachmentFilterCycle(t *testing.T) {
	var tm tea.Model = New(Options{Searcher: &recorder{}, Now: tuitest.Clock, Location: time.UTC})
	tm = tuitest.Drive(t, tm, tea.WindowSizeMsg{Width: 120, Height: 20}, tuitest.Key("tab"))
	tests := []struct {
		presses int
		want    string // in the filter label; "" = no attach entry at all
	}{{0, ""}, {1, "attach: any"}, {2, "attach: image"}, {3, "attach: document"}, {4, ""}, {5, "attach: any"}}
	pressed := 0
	for _, tt := range tests {
		for ; pressed < tt.presses; pressed++ {
			tm = tuitest.Send(t, tm, tuitest.Key("i"))
		}
		got := strings.SplitN(frameOf(tm), "\n", 2)[0] // the input line
		if tt.want == "" && strings.Contains(got, "attach:") || tt.want != "" && !strings.Contains(got, tt.want) {
			t.Errorf("after %d presses the input line is %q, want %q", tt.presses, got, tt.want)
		}
	}
}

func TestEmptySearchWithFilterListsAttachments(t *testing.T) {
	r := &recorder{}
	tm := newFilterModel(t, r, "")
	if got := frameOf(tm); !strings.Contains(got, "Type to search") {
		t.Fatalf("empty search without filter:\n%s", got)
	}
	n := r.count()
	tm = tuitest.Send(t, tm, tuitest.Key("i"))
	if r.count() != n+1 {
		t.Fatalf("the filter starts a search: %d queries before, %d after", n, r.count())
	}
	q := r.last()
	if q.Text != "" || q.Attachment == nil || q.Attachment.Kind != store.AttachmentAny {
		t.Errorf("query = %+v", q)
	}
	got := frameOf(tm)
	for _, want := range []string{"shots", "▣1", "1 hit"} {
		if !strings.Contains(got, want) {
			t.Errorf("frame lacks %q:\n%s", want, got)
		}
	}
	// Off again: the list is empty and the hint is back.
	for i := 0; i < 3; i++ {
		tm = tuitest.Send(t, tm, tuitest.Key("i"))
	}
	if got := frameOf(tm); !strings.Contains(got, "Type to search") || strings.Contains(got, "shots") {
		t.Errorf("filter off, empty input:\n%s", got)
	}
}

func TestAttachmentFilterKindAndActor(t *testing.T) {
	tests := []struct {
		name   string
		keys   []string
		kind   store.AttachmentKind
		actor  string
		onText bool
	}{
		{"any", []string{"i"}, store.AttachmentAny, "", false},
		{"images", []string{"i", "i"}, store.AttachmentImage, "", false},
		{"documents", []string{"i", "i", "i"}, store.AttachmentDocument, "", false},
		{"images of the human", []string{"a", "i", "i"}, store.AttachmentImage, "human", false},
		{"tool results", []string{"a", "a", "a", "a", "i", "i"}, store.AttachmentImage, "tool", false},
		{"with a search word", []string{"i", "i"}, store.AttachmentImage, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recorder{}
			query := ""
			if tt.onText {
				query = "shots"
			}
			tm := newFilterModel(t, r, query)
			for _, k := range tt.keys {
				tm = tuitest.Send(t, tm, tuitest.Key(k))
			}
			q := r.last()
			if q.Attachment == nil || q.Attachment.Kind != tt.kind || q.Attachment.Actor != tt.actor {
				t.Errorf("Attachment = %+v, want kind %q actor %q", q.Attachment, tt.kind, tt.actor)
			}
			if tt.onText != (q.Text != "") {
				t.Errorf("Text = %q", q.Text)
			}
		})
	}
}

func TestAttachmentFilterSkipsMemory(t *testing.T) {
	// Memory has no attachments: with the filter on it is not searched and
	// its group stays empty, so no unrelated hit pretends to carry one.
	r := &recorder{}
	tm := newFilterModel(t, r, "shots")
	tm = tuitest.Send(t, tm, tuitest.Key("s"))
	tm = tuitest.Send(t, tm, tuitest.Key("m"))
	before := r.count()
	tm = tuitest.Send(t, tm, tuitest.Key("i"))
	for _, q := range r.queries[before:] {
		if len(q.Types) == 1 && q.Types[0] == store.TypeMemory {
			t.Errorf("memory searched with the filter on: %+v", q)
		}
		if q.Attachment != nil && (len(q.Types) != 1 || q.Types[0] == store.TypeMemory) {
			t.Errorf("filter sent for %v", q.Types)
		}
	}
	if got := frameOf(tm); !strings.Contains(got, "memory 0") {
		t.Errorf("memory group is not empty:\n%s", got)
	}
}

func TestTypingWithEmptyInputKeepsTheFilter(t *testing.T) {
	r := &recorder{}
	tm := newFilterModel(t, r, "")
	tm = tuitest.Send(t, tm, tuitest.Key("i")) // filter on, list focus
	tm = tuitest.Send(t, tm, tuitest.Key("tab"))
	tm = tuitest.Send(t, tm, tuitest.Key("x"))
	tm = tuitest.Send(t, tm, tuitest.Key("backspace")) // empty again
	tm = tuitest.Send(t, tm, debounceFor(tm))
	if q := r.last(); q.Text != "" || q.Attachment == nil {
		t.Errorf("last query = %+v", q)
	}
	if got := frameOf(tm); !strings.Contains(got, "shots") {
		t.Errorf("hits of the filter are back:\n%s", got)
	}
}

// debounceFor is the debounce message of the current input sequence.
func debounceFor(tm tea.Model) tea.Msg { return debounceMsg{seq: tm.(Model).seq} }

func TestMarksFromTheHitsOwnAttachments(t *testing.T) {
	// The store fills SearchHit.Attachments for a filtered search; the list
	// marks them without asking again, also for a Searcher that cannot.
	tm := newFilterModel(t, &recorder{}, "shots")
	if got := frameOf(tm); !strings.Contains(got, viewer.AttachmentMark+"1") {
		t.Errorf("no mark:\n%s", got)
	}
}

func TestAttachmentFilterHelp(t *testing.T) {
	tm := newFilterModel(t, &recorder{}, "x")
	found := false
	for _, b := range tm.(Model).ShortHelp() {
		if b.Help().Desc == "attached" {
			found = true
		}
	}
	if !found {
		t.Error("the footer names the filter key")
	}
}
