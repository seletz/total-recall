package search

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// typeText sends every rune of s as its own key press.
func typeText(tm *teatest.TestModel, s string) {
	for _, r := range s {
		tuitest.Press(tm, string(r))
	}
}

func newModel(t *testing.T, st *store.Store, query string) Model {
	t.Helper()
	var s Searcher
	if st != nil {
		s = st
	}
	return New(Options{Searcher: s, Query: query, Now: tuitest.Clock, Location: testfixture.ActivityLocation})
}

// TestFrames drives the search view against the Activity fixture and
// compares the final frame with a golden file. Steps are key names (see
// tuitest.Key), "type:<text>" to type text, or "wait:<text>" to wait until
// the view shows text (results arrive asynchronously after the debounce).
func TestFrames(t *testing.T) {
	tests := []struct {
		name  string
		query string
		steps []string
	}{
		{"Empty", "", nil},
		{"Hits", "", []string{"type:release", "wait:hits ("}},
		{"NoHits", "", []string{"type:zzzz", "wait:no hits"}},
		{"InitialQuery", "release", []string{"wait:hits ("}},
		{"ClearedInput", "release", []string{"wait:hits (", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "wait:type to search"}},
		{"FocusList", "", []string{"type:release", "wait:hits (", "tab"}},
		{"FocusBackEsc", "", []string{"type:release", "wait:hits (", "tab", "esc"}},
		{"MoveDown", "", []string{"type:release", "wait:hits (", "tab", "j"}},
		{"MoveDownClamps", "", []string{"type:release", "wait:hits (", "tab", "down", "down", "down"}},
		{"MoveUpClamps", "", []string{"type:release", "wait:hits (", "tab", "j", "k", "up"}},
		{"ListTypingIgnored", "", []string{"type:release", "wait:hits (", "tab", "x"}},
		{"LongListScrolls", "", []string{"type:the", "wait:hits (", "tab", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j"}},
		{"Narrow", "", []string{"type:release", "wait:hits (", "resize"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tuitest.New(t, newModel(t, st, tt.query))
			for _, s := range tt.steps {
				switch {
				case strings.HasPrefix(s, "type:"):
					typeText(tm, strings.TrimPrefix(s, "type:"))
				case strings.HasPrefix(s, "wait:"):
					tuitest.WaitFor(t, tm, strings.TrimPrefix(s, "wait:"))
				case s == "resize":
					tuitest.Resize(tm, 60, 12)
				default:
					tuitest.Press(tm, s)
				}
			}
			final := tuitest.Final(t, tm).(Model)
			golden.RequireEqual(t, tuitest.Frame(final))
		})
	}
}

// TestNoStore renders the hint shown when the view has no database.
func TestNoStore(t *testing.T) {
	tm := tuitest.New(t, newModel(t, nil, "release"))
	final := tuitest.Final(t, tm).(Model)
	golden.RequireEqual(t, tuitest.Frame(final))
}

func TestFocused(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tests := []struct {
		name string
		keys []string
		want bool
	}{
		{"start", nil, true},
		{"tab to list", []string{"tab"}, false},
		{"tab twice stays in list", []string{"tab", "tab"}, false},
		{"esc back to input", []string{"tab", "esc"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m tea.Model = newModel(t, st, "")
			for _, k := range tt.keys {
				m, _ = m.Update(tuitest.Key(k))
			}
			if got := m.(Model).Focused(); got != tt.want {
				t.Errorf("Focused() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEnterOpensSession checks that enter emits OpenSession for the marked
// hit, from the list and from the input.
func TestEnterOpensSession(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	ctx := context.Background()
	hits, err := st.Search(ctx, "release", store.SearchFilter{})
	if err != nil || len(hits) < 2 {
		t.Fatalf("fixture search: %d hits, %v", len(hits), err)
	}
	tests := []struct {
		name string
		keys []string
		want store.SearchHit
	}{
		{"from input", []string{"enter"}, hits[0]},
		{"from list", []string{"tab", "enter"}, hits[0]},
		{"second hit", []string{"tab", "j", "enter"}, hits[1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m tea.Model = newModel(t, st, "release")
			m, cmd := m.Update(resultMsg{seq: m.(Model).seq, query: "release", hits: hits})
			if cmd != nil {
				t.Fatal("result produced a command")
			}
			for _, k := range tt.keys {
				m, cmd = m.Update(tuitest.Key(k))
			}
			if cmd == nil {
				t.Fatal("enter produced no command")
			}
			got, ok := cmd().(OpenSession)
			if !ok {
				t.Fatalf("enter produced %T, want OpenSession", cmd())
			}
			if got.SessionID != tt.want.Event.SessionID || got.EventID != tt.want.Event.ID {
				t.Errorf("OpenSession = %+v, want session %s event %s", got, tt.want.Event.SessionID, tt.want.Event.ID)
			}
		})
	}
	t.Run("no hits", func(t *testing.T) {
		var m tea.Model = newModel(t, st, "")
		if _, cmd := m.Update(tuitest.Key("enter")); cmd != nil {
			t.Error("enter without hits produced a command")
		}
	})
}

// fakeSearcher records every call and answers with one hit whose snippet is
// the query, so tests can tell which answer the model kept.
type fakeSearcher struct {
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	ctx   context.Context
	query string
	limit int
}

func (f *fakeSearcher) Search(ctx context.Context, query string, filter store.SearchFilter) ([]store.SearchHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{ctx: ctx, query: query, limit: filter.Limit})
	return []store.SearchHit{{Snippet: query}}, nil
}

func (f *fakeSearcher) byQuery() map[string]fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]fakeCall{}
	for _, c := range f.calls {
		out[c.query] = c
	}
	return out
}

// debounce feeds the model the debounce tick for its current sequence and
// returns the search command, if any.
func debounce(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(debounceMsg{seq: m.seq})
	return nm.(Model), cmd
}

func press(m Model, k string) (Model, tea.Cmd) {
	nm, cmd := m.Update(tuitest.Key(k))
	return nm.(Model), cmd
}

// TestStaleResultDiscarded is the sequence logic: a slower answer to an older
// query arriving after a newer one must not replace the newer hits, and the
// older query's context is cancelled by the newer input.
func TestStaleResultDiscarded(t *testing.T) {
	f := &fakeSearcher{}
	m := New(Options{Searcher: f, Now: tuitest.Clock})

	m, cmd := press(m, "a")
	if cmd == nil {
		t.Fatal("typing produced no debounce command")
	}
	m, search1 := debounce(t, m)
	if search1 == nil {
		t.Fatal("debounce produced no search command")
	}
	m, _ = press(m, "b")
	m, search2 := debounce(t, m)
	if search2 == nil {
		t.Fatal("second debounce produced no search command")
	}

	// The second query answers first, the first one later.
	r2, r1 := search2(), search1()
	m = update(t, m, r2)
	if got := snippets(m); got != "ab" {
		t.Fatalf("after fresh result: hits = %q, want %q", got, "ab")
	}
	m = update(t, m, r1)
	if got := snippets(m); got != "ab" {
		t.Errorf("after stale result: hits = %q, want %q", got, "ab")
	}

	calls := f.byQuery()
	if len(calls) != 2 {
		t.Fatalf("searcher called %d times, want 2", len(calls))
	}
	if calls["a"].ctx.Err() == nil {
		t.Error("first query's context not cancelled by the second input")
	}
	if calls["ab"].ctx.Err() != nil {
		t.Errorf("second query's context cancelled: %v", calls["ab"].ctx.Err())
	}
	for q, c := range calls {
		if c.limit != Limit {
			t.Errorf("limit of %q = %d, want %d", q, c.limit, Limit)
		}
	}
}

// TestDebounceSequence checks which ticks start a search: only the one for
// the current sequence, and none for an empty input.
func TestDebounceSequence(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		stale bool
		want  bool
	}{
		{"current tick searches", []string{"a"}, false, true},
		{"stale tick is ignored", []string{"a", "b"}, true, false},
		{"empty input never searches", []string{"a", "backspace"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Options{Searcher: &fakeSearcher{}, Now: tuitest.Clock})
			var seqs []uint64
			for _, k := range tt.keys {
				m, _ = press(m, k)
				seqs = append(seqs, m.seq)
			}
			seq := m.seq
			if tt.stale {
				seq = seqs[0]
			}
			nm, cmd := m.Update(debounceMsg{seq: seq})
			if (cmd != nil) != tt.want {
				t.Errorf("search command = %v, want %v", cmd != nil, tt.want)
			}
			if nm.(Model).searching != tt.want {
				t.Errorf("searching = %v, want %v", nm.(Model).searching, tt.want)
			}
		})
	}
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func snippets(m Model) string {
	var b strings.Builder
	for _, h := range m.hits {
		b.WriteString(h.Snippet)
	}
	return b.String()
}

// TestRenderSnippet checks the plain text of rendered snippets and that the
// marks become a style (escape sequences) instead of showing up as text.
func TestRenderSnippet(t *testing.T) {
	tests := []struct {
		name, in string
		width    int
		want     string
		styled   bool
	}{
		{"plain", "hello world", 20, "hello world", false},
		{"marks styled", "tag the " + hlStart + "release" + hlEnd, 40, "tag the release", true},
		{"newlines collapsed", "a\n\n  b\tc", 40, "a b c", false},
		{"truncated", "one two three four", 10, "one two t…", false},
		{"ellipsis kept", "…the " + hlStart + "release" + hlEnd + " notes…", 40, "…the release notes…", true},
		{"two matches", hlStart + "a" + hlEnd + " and " + hlStart + "b" + hlEnd, 40, "a and b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderSnippet(tt.in, tt.width)
			if plain := ansi.Strip(got); plain != tt.want {
				t.Errorf("renderSnippet(%q, %d) = %q, want %q", tt.in, tt.width, plain, tt.want)
			}
			if styled := strings.Contains(got, "\x1b["); styled != tt.styled {
				t.Errorf("renderSnippet(%q, %d) styled = %v, want %v", tt.in, tt.width, styled, tt.styled)
			}
			if strings.ContainsAny(got, hlStart+hlEnd) {
				t.Errorf("renderSnippet(%q, %d) leaks marks: %q", tt.in, tt.width, got)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name string
		m    Model
		want string
	}{
		{"no searcher", Model{}, "no database"},
		{"empty input", Model{opts: Options{Searcher: &fakeSearcher{}}}, "type to search"},
		{"searching", Model{opts: Options{Searcher: &fakeSearcher{}}, searching: true, query: "x"}, "searching…"},
		{"no hits", Model{opts: Options{Searcher: &fakeSearcher{}}, query: "x", took: 3 * time.Millisecond}, "no hits (3 ms)"},
		{"one hit", Model{opts: Options{Searcher: &fakeSearcher{}}, query: "x", hits: make([]store.SearchHit, 1)}, "1 hit (0 ms)"},
		{"hits", Model{opts: Options{Searcher: &fakeSearcher{}}, query: "x", hits: make([]store.SearchHit, 5), took: 12 * time.Millisecond}, "5 hits (12 ms)"},
		{"limit", Model{opts: Options{Searcher: &fakeSearcher{}}, query: "x", hits: make([]store.SearchHit, Limit)}, "200 hits, limit reached (0 ms)"},
		{"error", Model{opts: Options{Searcher: &fakeSearcher{}}, query: "x", err: context.DeadlineExceeded}, "error: context deadline exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.status(); got != tt.want {
				t.Errorf("status() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCursor checks the real cursor: behind the typed text while the input
// has the focus, none while the list has it.
func TestCursor(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tests := []struct {
		name  string
		query string
		keys  []string
		wantX int // -1: no cursor
	}{
		{"empty input", "", nil, 3},
		{"after text", "release", nil, 10},
		{"list has focus", "release", []string{"tab"}, -1},
		{"back in input", "release", []string{"tab", "esc"}, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m tea.Model = newModel(t, st, tt.query)
			m, _ = m.Update(tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height})
			for _, k := range tt.keys {
				m, _ = m.Update(tuitest.Key(k))
			}
			c := m.View().Cursor
			switch {
			case tt.wantX < 0 && c != nil:
				t.Errorf("cursor = %+v, want none", *c)
			case tt.wantX >= 0 && c == nil:
				t.Errorf("no cursor, want X=%d", tt.wantX)
			case tt.wantX >= 0 && (c.X != tt.wantX || c.Y != 0):
				t.Errorf("cursor = (%d,%d), want (%d,0)", c.X, c.Y, tt.wantX)
			}
		})
	}
}
