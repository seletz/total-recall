// Package search is the live full-text search view of the TUI: a text input
// on top, the hits of the archive in the left column and a preview of the
// exchange around the marked hit on the right.
//
// Every change of the input starts a search after a short pause (the
// debounce). Searches run as commands with a context; new input cancels the
// running query, and every answer carries the sequence number of the input it
// belongs to, so a slow answer to an older input is dropped. The preview
// follows the marked hit the same way with its own sequence.
package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
)

// Limit is the maximum number of hits one search fetches.
const Limit = 200

// DefaultDebounce is the pause after the last key press before a search starts.
const DefaultDebounce = 150 * time.Millisecond

// Searcher runs a full-text search; *store.Store implements it.
type Searcher interface {
	Search(ctx context.Context, query string, f store.SearchFilter) ([]store.SearchHit, error)
}

// Options configure the view.
type Options struct {
	// Searcher answers the queries; nil means no database, the view shows a hint.
	Searcher Searcher
	// Loader reads sessions for the preview; nil leaves the preview empty.
	Loader Loader
	// Query is the initial input; it is searched right away.
	Query string
	// Now is the clock used to time searches and to anchor the time filter;
	// nil means time.Now.
	Now func() time.Time
	// Location is the time zone hit times are shown in; nil means UTC.
	Location *time.Location
	// Debounce is the pause before a search starts; 0 means DefaultDebounce.
	Debounce time.Duration
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
}

func (o *Options) defaults() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Debounce <= 0 {
		o.Debounce = DefaultDebounce
	}
}

// OpenSession is sent upwards when the user opens the marked hit. It is not
// handled by this view; the app routes it to the session detail. Terms are
// the words of the query for highlighting.
type OpenSession struct {
	SessionID string
	EventID   string
	Terms     []string
}

// debounceMsg fires after the debounce pause; it starts a search when seq is
// still the current input sequence.
type debounceMsg struct{ seq uint64 }

// resultMsg is the answer of one search; it is applied only when seq is
// still the current input sequence.
type resultMsg struct {
	seq   uint64
	query string
	hits  []store.SearchHit
	took  time.Duration
	err   error
}

type focus int

const (
	focusInput focus = iota
	focusList
	focusRepo // the repo filter is typed in the status line
)

// Model is the search view.
type Model struct {
	opts    Options
	keys    KeyMap
	input   textinput.Model
	repoIn  textinput.Model
	focus   focus
	filters filters

	// seq counts input changes; debounce ticks and results carry the value
	// they were started for and are ignored when it moved on.
	seq    uint64
	cancel context.CancelFunc

	query     string // text of the shown hits (or the running search)
	hits      []store.SearchHit
	cursor    int // marked hit
	offset    int // first visible hit
	took      time.Duration
	err       error
	searching bool

	preview preview

	width, height int
}

// New returns the view for opts.
func New(opts Options) Model {
	opts.defaults()
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "type to search"
	in.SetValue(opts.Query)
	// The terminal's own cursor instead of a blinking virtual one: no timer
	// commands, and the view reports the position through tea.View.Cursor.
	in.SetVirtualCursor(false)
	in.Focus()
	repo := textinput.New()
	repo.Prompt = "repo: "
	repo.Placeholder = "substring of remote, root or cwd"
	repo.SetVirtualCursor(false)
	m := Model{opts: opts, keys: newKeyMap(opts.Keys), input: in, repoIn: repo}
	if opts.Query != "" {
		m.query = opts.Query
	}
	return m
}

// Title implements tui.View.
func (Model) Title() string { return "search" }

// Focused implements tui.Focused: true while the search input or the repo
// filter has the focus. Tab and shift+tab are handled by the view in every
// focus state, so they cycle input ↔ list and never reach the app.
func (m Model) Focused() bool { return m.focus != focusList }

// Init implements tea.Model: with an initial query, the first search runs
// without waiting for the debounce.
func (m Model) Init() tea.Cmd {
	if m.input.Value() == "" {
		return nil
	}
	return func() tea.Msg { return debounceMsg{seq: m.seq} }
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeInputs()
		m.clampOffset()
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseWheelMsg:
		return m.wheel(msg)
	case tea.MouseClickMsg:
		return m.click(msg)
	case debounceMsg:
		if msg.seq != m.seq || m.opts.Searcher == nil || m.input.Value() == "" {
			return m, nil
		}
		return m.search()
	case resultMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.searching = false
		m.query, m.hits, m.took, m.err = msg.query, msg.hits, msg.took, msg.err
		m.cursor, m.offset = 0, 0
		return m, m.follow()
	case previewTickMsg:
		if msg.seq != m.preview.seq {
			return m, nil
		}
		return m.loadPreview()
	case previewMsg:
		return m.previewResult(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// resizeInputs fits the search input beside the filter label and the repo
// input into the status line.
func (m *Model) resizeInputs() {
	label := len([]rune(m.filters.label()))
	m.input.SetWidth(max(m.width-len(m.input.Prompt)-label-4, 1))
	m.repoIn.SetWidth(max(m.width-len(m.repoIn.Prompt)-2, 1))
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.focus == focusRepo {
		return m.repoKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Open):
		return m, m.open()
	case key.Matches(msg, m.keys.Raw):
		m.filters.raw = !m.filters.raw
		return m.refilter()
	}
	if m.focus == focusList {
		switch {
		case key.Matches(msg, m.keys.Input):
			m.focus = focusInput
			m.input.Focus()
		case key.Matches(msg, m.keys.Up):
			return m, m.move(-1)
		case key.Matches(msg, m.keys.Down):
			return m, m.move(1)
		case key.Matches(msg, m.keys.Actor):
			m.filters.actor = (m.filters.actor + 1) % len(actors)
			return m.refilter()
		case key.Matches(msg, m.keys.Time):
			m.filters.span = (m.filters.span + 1) % len(spans)
			return m.refilter()
		case key.Matches(msg, m.keys.Repo):
			m.focus = focusRepo
			m.repoIn.SetValue(m.filters.repo)
			m.repoIn.CursorEnd()
			m.repoIn.Focus()
		}
		return m, nil
	}
	if key.Matches(msg, m.keys.List) {
		m.focus = focusList
		m.input.Blur()
		return m, nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() == before {
		return m, cmd
	}
	return m.changed(cmd)
}

// wheelLines is how far one wheel step scrolls the preview.
const wheelLines = 3

// wheel moves the mark when the pointer is over the list and scrolls the
// preview when it is over the preview column. The repo filter is modal and
// ignores the mouse.
func (m Model) wheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	var delta int
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return m, nil
	}
	if m.focus == focusRepo {
		return m, nil
	}
	if left, _ := m.columns(); msg.X > left {
		m.scrollPreview(delta * wheelLines)
		return m, nil
	}
	return m, m.move(delta)
}

// click marks the hit under the pointer and gives the list the focus; a
// click on the hit that is marked while the list has the focus opens it.
// A click on the input line moves the focus back into the input. Clicks on
// the preview, the rule and below the hits do nothing, so do clicks while
// the repo filter is typed.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.focus == focusRepo {
		return m, nil
	}
	if msg.Y == 0 {
		m.focus = focusInput
		m.input.Focus()
		return m, nil
	}
	if left, _ := m.columns(); msg.X >= left {
		return m, nil
	}
	row := msg.Y - (chromeRows - 1)
	if row < 0 || row/linesPerHit >= m.visibleHits() {
		return m, nil
	}
	i := m.offset + row/linesPerHit
	if i >= len(m.hits) {
		return m, nil
	}
	if i == m.cursor && m.focus == focusList {
		return m, m.open()
	}
	m.focus = focusList
	m.input.Blur()
	return m, m.move(i - m.cursor)
}

// repoKey edits the repo filter: enter applies it, esc drops the edit.
func (m Model) repoKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Confirm):
		m.focus = focusList
		m.repoIn.Blur()
		if v := strings.TrimSpace(m.repoIn.Value()); v != m.filters.repo {
			m.filters.repo = v
			return m.refilter()
		}
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		m.focus = focusList
		m.repoIn.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.repoIn, cmd = m.repoIn.Update(msg)
	return m, cmd
}

// refilter runs the search again after a filter changed, without debounce.
func (m Model) refilter() (tea.Model, tea.Cmd) {
	m.resizeInputs()
	m.seq++
	m.stop()
	if m.opts.Searcher == nil || m.input.Value() == "" {
		return m, nil
	}
	return m.search()
}

// changed registers new input: it invalidates running and pending work and
// schedules the debounce tick, or clears the hits when the input is empty.
func (m Model) changed(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.seq++
	m.stop()
	if m.input.Value() == "" {
		m.query, m.hits, m.err, m.took = "", nil, nil, 0
		m.cursor, m.offset = 0, 0
		return m, tea.Batch(cmd, m.follow())
	}
	seq := m.seq
	return m, tea.Batch(cmd, tea.Tick(m.opts.Debounce, func(time.Time) tea.Msg { return debounceMsg{seq: seq} }))
}

// stop cancels the running search, if any.
func (m *Model) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.searching = false
}

// search starts the query for the current input and filters.
func (m Model) search() (tea.Model, tea.Cmd) {
	m.stop()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.searching = true
	m.query = m.input.Value()
	seq, query, now, s := m.seq, m.query, m.opts.Now, m.opts.Searcher
	f := store.SearchFilter{
		SessionFilter:  store.SessionFilter{Limit: Limit},
		HighlightStart: hlStart,
		HighlightEnd:   hlEnd,
	}
	m.filters.apply(&f, now())
	return m, func() tea.Msg {
		start := now()
		hits, err := s.Search(ctx, query, f)
		return resultMsg{seq: seq, query: query, hits: hits, took: now().Sub(start), err: err}
	}
}

// open emits OpenSession for the marked hit.
func (m Model) open() tea.Cmd {
	h := m.marked()
	if h == nil {
		return nil
	}
	ts := terms(m.query, m.filters.raw)
	return func() tea.Msg { return OpenSession{SessionID: h.Event.SessionID, EventID: h.Event.ID, Terms: ts} }
}

// move marks the hit delta entries away and lets the preview follow.
func (m *Model) move(delta int) tea.Cmd {
	if len(m.hits) == 0 {
		return nil
	}
	cursor := min(max(m.cursor+delta, 0), len(m.hits)-1)
	if cursor == m.cursor {
		return nil
	}
	m.cursor = cursor
	m.clampOffset()
	return m.follow()
}

// clampOffset scrolls the list so the marked hit is visible.
func (m *Model) clampOffset() {
	rows := m.visibleHits()
	if rows <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(min(m.offset, max(len(m.hits)-rows, 0)), 0)
}

// ShortHelp implements help.KeyMap; it follows the focus.
func (m Model) ShortHelp() []key.Binding {
	switch m.focus {
	case focusList:
		return []key.Binding{m.keys.Move, m.keys.Open, m.keys.Actor, m.keys.Time, m.keys.Repo, m.keys.Raw, m.keys.Input}
	case focusRepo:
		return []key.Binding{m.keys.Confirm, m.keys.Cancel}
	}
	return []key.Binding{m.keys.List, m.keys.Open, m.keys.Raw}
}

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.keys.List, m.keys.Input, m.keys.Open, m.keys.Up, m.keys.Down},
		{m.keys.Actor, m.keys.Time, m.keys.Repo, m.keys.Raw},
	}
}

// status is the text of the bottom line: hit count and duration, or why
// there are no hits.
func (m Model) status() string {
	switch {
	case m.opts.Searcher == nil:
		return "no database"
	case m.err != nil:
		return "error: " + m.err.Error()
	case m.searching:
		return "searching…"
	case m.query == "":
		return "type to search"
	}
	ms := fmt.Sprintf(" (%d ms)", m.took.Milliseconds())
	switch n := len(m.hits); {
	case n == 0:
		return "no hits" + ms
	case n == 1:
		return "1 hit" + ms
	case n >= Limit:
		return fmt.Sprintf("%d hits, limit reached%s", n, ms)
	default:
		return fmt.Sprintf("%d hits%s", n, ms)
	}
}

// hint is shown in the list column instead of hits.
func (m Model) hint() string {
	switch {
	case m.opts.Searcher == nil:
		return "No database open."
	case m.err != nil:
		return "The search failed; see the status line."
	case m.query == "":
		return "Type to search; tab moves to the hits."
	case m.searching && len(m.hits) == 0:
		return ""
	case len(m.hits) == 0:
		return "No hits for " + strings.TrimSpace(m.query) + "."
	}
	return ""
}
