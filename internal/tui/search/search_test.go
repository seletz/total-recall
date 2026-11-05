package search

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// probe wraps the view so a test can wait until the preview of the marked
// hit has been loaded: it counts the key presses the view has handled and
// records whether the preview is ready after every message.
type probe struct {
	Model
	st *probeState
}

type probeState struct {
	mu      sync.Mutex
	keys    int  // key presses handled
	results int  // fresh search results applied
	ready   bool // preview settled after the last message
}

func (p probe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	fresh := false
	if r, ok := msg.(resultMsg); ok {
		fresh = r.seq == p.seq
	}
	m, cmd := p.Model.Update(msg)
	p.Model = m.(Model)
	p.st.mu.Lock()
	defer p.st.mu.Unlock()
	if _, ok := msg.(tea.KeyPressMsg); ok {
		p.st.keys++
	}
	if fresh {
		p.st.results++
	}
	p.st.ready = p.previewReady()
	return p, cmd
}

// previewReady reports whether the right column is settled: no hit is
// marked, there is no loader, or the preview holds the marked hit.
func (m Model) previewReady() bool {
	h := m.marked()
	return h == nil || m.opts.Loader == nil || m.preview.eventID == h.Event.ID
}

// wait blocks until the view has handled keys key presses and results
// fresh search results and, with preview, the preview is ready; it fails
// the test after the timeout.
func wait(t *testing.T, st *probeState, keys, results int, preview bool) {
	t.Helper()
	deadline := time.Now().Add(tuitest.Timeout)
	for time.Now().Before(deadline) {
		st.mu.Lock()
		ok := st.keys >= keys && st.results >= results && (!preview || st.ready)
		st.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("view did not settle (keys %d, results %d, preview %v)", keys, results, preview)
}

func newModel(t *testing.T, st *store.Store, query string) Model {
	t.Helper()
	opts := Options{Query: query, Now: tuitest.Clock, Location: testfixture.ActivityLocation}
	if st != nil {
		opts.Searcher, opts.Loader = st, st
	}
	return New(opts)
}

// run drives the view against st with steps and returns the final model.
// Steps are key names (see tuitest.Key), "type:<text>" to type text,
// "result" to wait until the next search result has been applied (results
// arrive asynchronously after the debounce), "preview" to wait until the
// preview of the marked hit is loaded, "wait:<text>" to wait until the
// output shows text, and "resize" for a smaller window.
func run(t *testing.T, st *store.Store, query string, steps []string) Model {
	t.Helper()
	p := probe{Model: newModel(t, st, query), st: &probeState{}}
	tm := tuitest.New(t, p)
	keys, results := 0, 0
	for _, s := range steps {
		switch {
		case strings.HasPrefix(s, "type:"):
			text := strings.TrimPrefix(s, "type:")
			for _, r := range text {
				tuitest.Press(tm, string(r))
			}
			keys += len([]rune(text))
		case strings.HasPrefix(s, "wait:"):
			tuitest.WaitFor(t, tm, strings.TrimPrefix(s, "wait:"))
		case s == "result":
			results++
			wait(t, p.st, keys, results, false)
		case s == "preview":
			wait(t, p.st, keys, results, true)
		case s == "resize":
			tuitest.Resize(tm, 60, 12)
		default:
			tuitest.Press(tm, s)
			keys++
		}
	}
	return tuitest.Final(t, tm).(probe).Model
}

// TestFrames drives the search view against the Activity fixture and
// compares the final frame with a golden file; see run for the steps.
func TestFrames(t *testing.T) {
	tests := []struct {
		name  string
		query string
		steps []string
	}{
		{"Empty", "", nil},
		{"Hits", "", []string{"type:release", "result", "preview"}},
		{"NoHits", "", []string{"type:zzzz", "result"}},
		{"InitialQuery", "release", []string{"result", "preview"}},
		{"ClearedInput", "release", []string{"result", "preview", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "wait:type to search", "preview"}},
		{"FocusList", "", []string{"type:release", "result", "tab", "preview"}},
		{"FocusBackEsc", "", []string{"type:release", "result", "tab", "esc", "preview"}},
		{"MoveDown", "", []string{"type:release", "result", "tab", "j", "preview"}},
		{"MoveDownClamps", "", []string{"type:release", "result", "tab", "down", "down", "down", "preview"}},
		{"MoveUpClamps", "", []string{"type:release", "result", "tab", "j", "k", "up", "preview"}},
		{"ListTypingIgnored", "", []string{"type:release", "result", "tab", "x", "preview"}},
		{"LongListScrolls", "", []string{"type:the", "result", "tab", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "preview"}},
		{"Narrow", "", []string{"type:release", "result", "preview", "resize"}},
		{"PreviewToolHit", "", []string{"type:tag", "result", "tab", "j", "preview"}},
		{"FilterActorHuman", "", []string{"type:release", "result", "tab", "a", "result", "preview"}},
		{"FilterActorAgent", "", []string{"type:release", "result", "tab", "a", "result", "a", "result", "preview"}},
		{"FilterActorPeer", "", []string{"type:release", "result", "tab", "a", "result", "a", "result", "a", "result"}},
		{"FilterActorWraps", "", []string{"type:release", "result", "tab", "a", "result", "a", "result", "a", "result", "a", "result", "preview"}},
		{"FilterTime7d", "", []string{"type:release", "result", "tab", "t", "result"}},
		{"FilterTimeCycle", "", []string{"type:release", "result", "tab", "t", "result", "t", "result", "t", "result", "t", "result", "preview"}},
		{"FilterRepoTyping", "", []string{"type:release", "result", "preview", "tab", "r", "type:bet"}},
		{"FilterRepoNoHits", "", []string{"type:release", "result", "tab", "r", "type:alpha", "enter", "result"}},
		{"FilterRepoHits", "", []string{"type:the", "result", "tab", "r", "type:gamma", "enter", "result", "preview"}},
		{"FilterRepoEsc", "", []string{"type:release", "result", "tab", "r", "type:alpha", "esc", "preview"}},
		{"FilterRepoUnchanged", "", []string{"type:release", "result", "tab", "r", "enter", "preview"}},
		{"FilterRepoCleared", "", []string{"type:release", "result", "tab", "r", "type:alpha", "enter", "result", "r", "backspace", "backspace", "backspace", "backspace", "backspace", "enter", "result", "preview"}},
		{"FilterRaw", "", []string{"type:release OR notes", "result", "ctrl+r", "result", "preview"}},
		{"FilterRawFromList", "", []string{"type:release OR notes", "result", "tab", "ctrl+r", "result", "ctrl+r", "result", "preview"}},
		{"FilterActorFromInputIgnored", "", []string{"type:release", "result", "a", "result"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			final := run(t, st, tt.query, tt.steps)
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
		{"tab cycles back to input", []string{"tab", "tab"}, true},
		{"shift+tab to list", []string{"shift+tab"}, false},
		{"shift+tab back to input", []string{"tab", "shift+tab"}, true},
		{"esc back to input", []string{"tab", "esc"}, true},
		{"esc in input stays", []string{"esc"}, true},
		{"repo filter typed", []string{"tab", "r"}, true},
		{"repo filter applied", []string{"tab", "r", "enter"}, false},
		{"repo filter applied with tab", []string{"tab", "r", "tab"}, false},
		{"repo filter applied with shift+tab", []string{"tab", "r", "shift+tab"}, false},
		{"repo filter cancelled", []string{"tab", "r", "esc"}, false},
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

// focusFrame is the frame followed by what the app shows of the focus and
// the plain frame cannot: the cursor position, whether the marked hit sits
// on the selection background (the escape sequence right before the mark
// sets a background), and the short help of the focused element (the
// footer).
func focusFrame(m Model) string {
	v := m.View()
	cursor := "none"
	if v.Cursor != nil {
		cursor = fmt.Sprintf("%d,%d", v.Cursor.X, v.Cursor.Y)
	}
	mark := "plain"
	if i := strings.Index(v.Content, "▸"); i >= 0 && strings.Contains(v.Content[strings.LastIndex(v.Content[:i], "\x1b["):i], "48;2;") {
		mark = "inverted"
	}
	h := help.New()
	h.SetWidth(tuitest.Width)
	return tuitest.Frame(m) + "cursor: " + cursor + "\nmark: " + mark + "\nkeys: " + ansi.Strip(h.ShortHelpView(m.ShortHelp())) + "\n"
}

// TestFocusFrames pins the focus contract (docs/reference/30-tui.md, Fokus): tab and
// shift+tab cycle input ↔ list without leaving the view, esc goes one level
// back, letters in the input are text, the repo filter is applied by enter
// or tab and dropped by esc, and the focused element is the one with the
// cursor or the inverted mark. The view is driven synchronously; see
// focusFrame for what the golden holds besides the frame.
func TestFocusFrames(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"InputMarksHit", nil},
		{"TabToList", []string{"tab"}},
		{"TabCyclesToInput", []string{"tab", "tab"}},
		{"ShiftTabToList", []string{"shift+tab"}},
		{"ShiftTabCyclesToInput", []string{"tab", "shift+tab"}},
		{"EscToInput", []string{"tab", "esc"}},
		{"EscInInputIgnored", []string{"esc"}},
		{"FilterKeysAreText", []string{"a", "t", "r"}},
		{"GlobalKeysAreText", []string{"q", "1", "2", "?"}},
		{"MarkKeptAcrossFocus", []string{"tab", "j", "tab"}},
		{"RepoTyping", []string{"tab", "r", "x"}},
		{"RepoTabApplies", []string{"tab", "r", "x", "tab"}},
		{"RepoEnterApplies", []string{"tab", "r", "x", "enter"}},
		{"RepoEscDiscards", []string{"tab", "r", "x", "esc"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{Searcher: st, Loader: st, Query: "release", Now: tuitest.Clock,
				Location: testfixture.ActivityLocation, Debounce: time.Millisecond}
			msgs := append([]tea.Msg{tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}}, tuitest.Keys(tt.keys...)...)
			final := tuitest.Drive(t, New(opts), msgs...)
			golden.RequireEqual(t, focusFrame(final.(Model)))
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
			// The result also schedules the preview; that command is not
			// run here.
			m, _ = m.Update(resultMsg{seq: m.(Model).seq, query: "release", hits: hits})
			var cmd tea.Cmd
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
			if !slices.Equal(got.Terms, []string{"release"}) {
				t.Errorf("OpenSession.Terms = %q, want [release]", got.Terms)
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

// searchResult runs the command of a search and returns its resultMsg; the
// command is a batch of the query and the spinner's first tick.
func searchResult(t *testing.T, cmd tea.Cmd) resultMsg {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("search did not return a batch")
	}
	for _, c := range batch {
		if r, ok := c().(resultMsg); ok {
			return r
		}
	}
	t.Fatal("batch holds no search result")
	return resultMsg{}
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
	r2, r1 := searchResult(t, search2), searchResult(t, search1)
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
			got := New(Options{}).renderSnippet(tt.in, tt.width)
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

// TestStatus checks the plain text of the status line; while a search runs
// the spinner's first frame turns in front of it.
func TestStatus(t *testing.T) {
	tests := []struct {
		name     string
		searcher bool
		set      func(m *Model)
		want     string
	}{
		{"no searcher", false, nil, "no database"},
		{"empty input", true, nil, "type to search"},
		{"searching", true, func(m *Model) { m.searching, m.query = true, "x" }, "⠋ searching…"},
		{"no hits", true, func(m *Model) { m.query, m.took = "x", 3*time.Millisecond }, "no hits (3 ms)"},
		{"one hit", true, func(m *Model) { m.query, m.hits = "x", make([]store.SearchHit, 1) }, "1 hit (0 ms)"},
		{"hits", true, func(m *Model) { m.query, m.hits, m.took = "x", make([]store.SearchHit, 5), 12*time.Millisecond }, "5 hits (12 ms)"},
		{"limit", true, func(m *Model) { m.query, m.hits = "x", make([]store.SearchHit, Limit) }, "200 hits, limit reached (0 ms)"},
		{"error", true, func(m *Model) { m.query, m.err = "x", context.DeadlineExceeded }, "error: context deadline exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{Now: tuitest.Clock}
			if tt.searcher {
				opts.Searcher = &fakeSearcher{}
			}
			m := New(opts)
			if tt.set != nil {
				tt.set(&m)
			}
			if got := ansi.Strip(m.status()); got != tt.want {
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

// TestSetKeys: new bindings act at once; the query and the hits stay.
func TestSetKeys(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	km, err := keys.Resolve(map[string][]string{keys.SearchActor: {"A"}})
	if err != nil {
		t.Fatal(err)
	}
	m := tuitest.Drive(t, newModel(t, st, "release"), tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}, tuitest.Key("tab")).(Model)
	m = m.SetKeys(km).(Model)
	f := tuitest.Frame(m)
	if !strings.Contains(f, "> release") {
		t.Errorf("query lost:\n%s", f)
	}
	before := tuitest.Frame(tuitest.Send(t, m, tuitest.Key("a")).(Model))
	after := tuitest.Frame(tuitest.Send(t, m, tuitest.Key("A")).(Model))
	if before != f {
		t.Errorf("a still switches the actor filter:\n%s", before)
	}
	if after == f {
		t.Errorf("A does not switch the actor filter:\n%s", after)
	}
	if h := m.keys.Actor.Help(); h.Key != "A" {
		t.Errorf("actor help = %q", h.Key)
	}
}
