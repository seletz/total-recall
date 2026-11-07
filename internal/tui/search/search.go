// Package search is the live full-text search view of the TUI: a text input
// on top, a rule with the type switcher and the project pin, the hits of the
// archive in the left column and a preview of the marked hit on the right.
//
// The search covers the object types of the store (event, session, memory):
// the switcher selects them, the hits are listed in one group per type, each
// in the format of its type, and the preview picks a viewer (package viewer)
// by the type of the marked hit. Event is the default; with it alone the
// view lists the hits of the events as it always did. The pin restricts every
// type to one project, chosen from the project list or taken from the marked
// hit.
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
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/memview"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// Limit is the maximum number of hits one search fetches.
const Limit = 200

// DefaultDebounce is the pause after the last key press before a search starts.
const DefaultDebounce = 150 * time.Millisecond

// Searcher runs a full-text search over the object types;
// *store.Store implements it.
type Searcher interface {
	SearchTyped(ctx context.Context, q store.TypedQuery) ([]store.TypeHits, error)
}

// Options configure the view.
type Options struct {
	// Searcher answers the queries; nil means no database, the view shows a hint.
	Searcher Searcher
	// Loader reads what the preview shows; nil leaves the preview empty.
	Loader Loader
	// Projects and Resolver name the projects (the pin, the project
	// column of memory hits); Projects nil means no project list, Home
	// shortens paths in derived names.
	Projects project.Source
	Resolver project.Resolver
	Home     string
	// Query is the initial input; it is searched right away.
	Query string
	// Now is the clock used to time searches and to anchor the time filter;
	// nil means time.Now.
	Now func() time.Time
	// Location is the time zone hit times are shown in; nil means UTC.
	Location *time.Location
	// Debounce is the pause before a search starts; 0 means DefaultDebounce.
	Debounce time.Duration
	// PreviewDebounce is the pause before the marked hit is loaded for the
	// preview; 0 means the PreviewDebounce constant.
	PreviewDebounce time.Duration
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
	if o.PreviewDebounce <= 0 {
		o.PreviewDebounce = PreviewDebounce
	}
}

// OpenSession is sent upwards when the user opens the marked hit (an event,
// or a session: then EventID is empty). It is not handled by this view; the
// app routes it to the session detail. Terms are the words of the query for
// highlighting.
type OpenSession struct {
	SessionID string
	EventID   string
	Terms     []string
}

// debounceMsg fires after the debounce pause; it starts a search when seq is
// still the current input sequence.
type debounceMsg struct{ seq uint64 }

// resultMsg is the answer of one search; it is applied only when seq is
// still the current input sequence. A quiet result comes from a reload:
// the mark stays on the hit it was on.
type resultMsg struct {
	seq    uint64
	query  string
	groups []store.TypeHits
	took   time.Duration
	err    error
	quiet  bool
}

type focus int

const (
	focusInput focus = iota
	focusList
	focusRepo // the repo filter is typed in the status line
	focusPick // a project is chosen from the list
)

// Model is the search view.
type Model struct {
	opts    Options
	keys    KeyMap
	input   textinput.Model
	repoIn  textinput.Model
	focus   focus
	filters filters
	// types are the selected object types (display order), pin the
	// project the search is restricted to, cat the project catalog (nil
	// until loaded) and pick the state of the project choice.
	types []store.HitType
	pin   *pin
	cat   *project.Catalog
	pick  picker

	// seq counts input changes; debounce ticks and results carry the value
	// they were started for and are ignored when it moved on.
	seq    uint64
	cancel context.CancelFunc

	query     string // text of the shown hits (or the running search)
	groups    []store.TypeHits
	hits      []store.Hit // the hits of all groups, in group order
	rows      []row       // the list: group headers and hits
	cursor    int         // marked hit
	offset    int         // first visible row
	took      time.Duration
	err       error
	searching bool

	preview preview

	width, height int
	// th is the theme; dark until the terminal answers the app's
	// background colour query. spin turns in the status line while a
	// search runs.
	th   theme.Theme
	spin spinner.Model
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
	m := Model{opts: opts, keys: newKeyMap(opts.Keys), input: in, repoIn: repo, types: []store.HitType{store.TypeEvent}, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
	m.setTheme(theme.New(true))
	if opts.Query != "" {
		m.query = opts.Query
	}
	return m
}

// setTheme applies th to the view and its inputs: the prompt of the
// focused input in the title style, blurred muted.
func (m *Model) setTheme(th theme.Theme) {
	m.th = th
	m.spin.Style = th.Spinner
	for _, in := range []*textinput.Model{&m.input, &m.repoIn} {
		st := textinput.DefaultStyles(th.Dark)
		st.Focused.Prompt, st.Blurred.Prompt = th.Title, th.Muted
		st.Focused.Placeholder, st.Blurred.Placeholder = th.Muted, th.Muted
		in.SetStyles(st)
	}
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
		return m.loadCatalog()
	}
	return tea.Batch(m.loadCatalog(), func() tea.Msg { return debounceMsg{seq: m.seq} })
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
		return m.search(false)
	case resultMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		return m.result(msg)
	case catalogMsg:
		if msg.err == nil {
			m.cat = msg.cat
		}
		return m, nil
	case previewTickMsg:
		if msg.seq != m.preview.seq {
			return m, nil
		}
		return m.loadPreview()
	case previewMsg:
		return m.previewResult(msg)
	case tea.BackgroundColorMsg:
		m.setTheme(theme.New(msg.IsDark()))
		return m, nil
	case spinner.TickMsg:
		// The spinner runs only while a search does; a tick after the
		// result stops it.
		if !m.searching {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
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
	if m.focus == focusPick {
		return m.pickKey(msg)
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
		case key.Matches(msg, m.keys.Pin):
			return m.togglePin()
		case key.Matches(msg, m.keys.Project):
			return m.openPicker(), nil
		default:
			for _, t := range searchableTypes() {
				if key.Matches(msg, m.keys.typeKey(t)) {
					return m.toggleTypeAndSearch(t)
				}
			}
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
	if m.focus == focusPick {
		m.pick.cursor = min(max(m.pick.cursor+delta, 0), len(m.pick.entries)-1)
		m.pick.scroll(m.bodyRows())
		return m, nil
	}
	return m, m.move(delta)
}

// click marks the hit under the pointer and gives the list the focus; a
// click on the hit that is marked while the list has the focus opens it.
// A click on the preview opens the marked hit like enter, whichever
// element has the focus. A click on the input line moves the focus back
// into the input. A click on the rule row works the type switcher and the
// project pin (clickRule). Clicks on the separator, the group headers, the
// status line and below the hits do nothing, so do clicks while the repo
// filter is typed. While the project choice is open, a click on a project
// applies it.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.focus == focusRepo {
		return m, nil
	}
	if msg.Y == 0 {
		if m.focus == focusPick {
			return m, nil
		}
		m.focus = focusInput
		m.input.Focus()
		return m, nil
	}
	if msg.Y == 1 {
		return m.clickRule(msg.X)
	}
	line := msg.Y - (chromeRows - 1)
	if line < 0 || line >= m.bodyRows() {
		return m, nil
	}
	left, _ := m.columns()
	if m.focus == focusPick {
		if msg.X < left && m.pick.offset+line < len(m.pick.entries) {
			return m.applyPick(m.pick.offset + line)
		}
		return m, nil
	}
	if msg.X > left {
		return m, m.open()
	} else if msg.X == left {
		return m, nil
	}
	r := m.rowAt(line)
	if r < 0 || m.rows[r].header {
		return m, nil
	}
	i := m.rows[r].hit
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
	return m.search(false)
}

// changed registers new input: it invalidates running and pending work and
// schedules the debounce tick, or clears the hits when the input is empty.
func (m Model) changed(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.seq++
	m.stop()
	if m.input.Value() == "" {
		m.query, m.err, m.took = "", nil, 0
		m.setGroups(nil)
		m.cursor, m.offset = 0, 0
		return m, tea.Batch(cmd, m.follow())
	}
	seq := m.seq
	return m, tea.Batch(cmd, tea.Tick(m.opts.Debounce, func(time.Time) tea.Msg { return debounceMsg{seq: seq} }))
}

// result applies the answer of a search. A fresh search marks the first
// hit; a reload keeps the mark on the same hit (by event ID, else by
// position), keeps the scroll offset and refreshes the preview in place.
func (m Model) result(msg resultMsg) (tea.Model, tea.Cmd) {
	m.searching = false
	var marked string
	if h := m.marked(); h != nil {
		marked = hitKey(*h)
	}
	m.query, m.took, m.err = msg.query, msg.took, msg.err
	m.setGroups(msg.groups)
	if !msg.quiet {
		m.cursor, m.offset = 0, 0
		return m, m.follow()
	}
	m.cursor = min(m.cursor, max(len(m.hits)-1, 0))
	for i, h := range m.hits {
		if hitKey(h) == marked {
			m.cursor = i
			break
		}
	}
	m.clampOffset()
	if h := m.marked(); h != nil && hitKey(*h) == marked {
		return m, m.refreshPreview()
	}
	return m, m.follow()
}

// Reload runs the current query again without debounce and without the
// spinner, keeping input, focus, mark and scroll position; the preview
// follows. Without a database or without a query there is nothing to do.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	if m.opts.Searcher == nil || m.input.Value() == "" {
		return m, nil
	}
	m.seq++
	m.stop()
	r, cmd := m.search(true)
	return r, tea.Batch(cmd, m.loadCatalog())
}

// stop cancels the running search, if any.
func (m *Model) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.searching = false
}

// search starts the query for the current input and filters, and the
// spinner. The query comes first in the batch: run synchronously (tests),
// its result stops the spinner before the spinner's first tick arrives. A
// quiet search (reload) shows neither the spinner nor "searching…": the
// hits stay until the answer replaces them.
func (m Model) search(quiet bool) (tea.Model, tea.Cmd) {
	m.stop()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	seq, now, s := m.seq, m.opts.Now, m.opts.Searcher
	query := m.input.Value()
	f := m.filter(now())
	types, scope := m.types, m.pinScope()
	run := func(quiet bool) tea.Msg {
		start := now()
		groups, err := searchTypes(ctx, s, query, f, types, scope)
		return resultMsg{seq: seq, query: query, groups: groups, took: now().Sub(start), err: err, quiet: quiet}
	}
	if quiet {
		return m, func() tea.Msg { return run(true) }
	}
	m.searching = true
	m.query = query
	return m, tea.Batch(func() tea.Msg { return run(false) }, m.spin.Tick)
}

// pinScope is the project scope of the pin, nil without one.
func (m Model) pinScope() *store.ProjectScope {
	if m.pin == nil {
		return nil
	}
	return m.pin.scope
}

// searchTypes runs the query for every selected type, one after the
// other, and returns the groups in the order of types. Every type gets
// only the filters it applies (store.Supports): the actor filter does not
// reach memory, the raw syntax does not reach sessions, so a filter never
// turns a type into an error.
func searchTypes(ctx context.Context, s Searcher, query string, f store.SearchFilter, types []store.HitType, scope *store.ProjectScope) ([]store.TypeHits, error) {
	var out []store.TypeHits
	for _, t := range types {
		q := store.TypedQuery{Text: query, Types: []store.HitType{t}, HighlightStart: f.HighlightStart, HighlightEnd: f.HighlightEnd}
		q.Limit = f.Limit
		if store.Supports(t, store.FilterSince) {
			q.Since = f.Since
		}
		if store.Supports(t, store.FilterRepo) {
			q.Repo = f.Repo
		}
		if store.Supports(t, store.FilterActor) {
			q.Actor = f.Actor
		}
		if store.Supports(t, store.FilterRaw) {
			q.Raw = f.Raw
		}
		if store.Supports(t, store.FilterProject) {
			q.Project = scope
		}
		g, err := s.SearchTyped(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, g...)
	}
	return out, nil
}

// setGroups stores the groups of an answer, the flat list of their hits
// and the rows of the list.
func (m *Model) setGroups(groups []store.TypeHits) {
	m.groups = groups
	m.hits = m.hits[:0:0]
	for _, g := range groups {
		m.hits = append(m.hits, g.Hits...)
	}
	m.buildRows()
}

// toggleTypeAndSearch switches type t on or off and searches again.
func (m Model) toggleTypeAndSearch(t store.HitType) (tea.Model, tea.Cmd) {
	if !m.toggleType(t) {
		return m, nil
	}
	return m.refilter()
}

// clickRule handles a click at column x of the rule row: a type toggles,
// the project label opens the project choice, the sign drops the pin.
func (m Model) clickRule(x int) (tea.Model, tea.Cmd) {
	if m.focus == focusPick {
		return m, nil
	}
	left, right := m.columns()
	_, z := m.ruleRow(left, right)
	if i := zone.At(z.types, x); i >= 0 {
		return m.toggleTypeAndSearch(searchableTypes()[i])
	}
	switch {
	case z.clear.Contains(x):
		m.pin = nil
		return m.refilter()
	case z.project.Contains(x):
		return m.openPicker(), nil
	}
	return m, nil
}

// filter is the store filter of the current filter settings at now.
func (m Model) filter(now time.Time) store.SearchFilter {
	f := store.SearchFilter{
		SessionFilter:  store.SessionFilter{Limit: Limit},
		HighlightStart: hlStart,
		HighlightEnd:   hlEnd,
	}
	m.filters.apply(&f, now)
	return f
}

// open emits OpenSession for the marked hit, memview.Open for a memory
// file: the app shows the memory of its project with the file marked.
func (m Model) open() tea.Cmd {
	h := m.marked()
	if h == nil {
		return nil
	}
	ts := terms(m.query, m.filters.raw)
	switch {
	case h.Event != nil:
		o := OpenSession{SessionID: h.Event.Event.SessionID, EventID: h.Event.Event.ID, Terms: ts}
		return func() tea.Msg { return o }
	case h.Session != nil:
		o := OpenSession{SessionID: h.Session.ID, Terms: ts}
		return func() tea.Msg { return o }
	case h.Memory != nil:
		o := memview.Open{ProjectID: h.Memory.ProjectID, Project: m.projectName(*h), Harness: h.Memory.Harness, Path: h.Memory.Path, Terms: ts}
		return func() tea.Msg { return o }
	}
	return nil
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

// clampOffset scrolls the list so the marked hit is visible; the header
// of its group comes along when it is just above.
func (m *Model) clampOffset() {
	if m.bodyRows() <= 0 || len(m.rows) == 0 {
		m.offset = 0
		return
	}
	r := m.rowOf(m.cursor)
	if r < 0 {
		m.offset = 0
		return
	}
	if r < m.offset {
		m.offset = r
	}
	for m.offset < r && r >= m.offset+m.visibleRows(m.offset) {
		m.offset++
	}
	if r > 0 && m.rows[r-1].header && m.offset == r && m.visibleRows(r-1) > 1 {
		m.offset = r - 1
	}
	// Never leave empty space below the last row while rows are above.
	for m.offset > 0 && m.visibleRows(m.offset-1) >= len(m.rows)-(m.offset-1) {
		m.offset--
	}
	m.offset = max(m.offset, 0)
}

// ShortHelp implements help.KeyMap; it follows the focus.
func (m Model) ShortHelp() []key.Binding {
	switch m.focus {
	case focusList:
		return []key.Binding{m.keys.Move, m.keys.Open, m.keys.Types, m.keys.Actor, m.keys.Time, m.keys.Repo, m.keys.Raw, m.keys.Input}
	case focusRepo, focusPick:
		return []key.Binding{m.keys.Confirm, m.keys.Cancel}
	}
	return []key.Binding{m.keys.List, m.keys.Open, m.keys.Raw}
}

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.keys.List, m.keys.Input, m.keys.Open, m.keys.Up, m.keys.Down},
		{m.keys.Actor, m.keys.Time, m.keys.Repo, m.keys.Raw},
		{m.keys.TypeEvent, m.keys.TypeSession, m.keys.TypeMemory, m.keys.Pin, m.keys.Project},
	}
}

// status is the text of the bottom line: hit count and duration, or why
// there are no hits; the spinner turns in front of "searching…".
func (m Model) status() string {
	switch {
	case m.opts.Searcher == nil:
		return "no database"
	case m.err != nil:
		return m.th.Error.Render("error: " + m.err.Error())
	case m.searching:
		return m.spin.View() + " searching…"
	case m.query == "":
		return "type to search"
	}
	ms := fmt.Sprintf(" (%d ms)", m.took.Milliseconds())
	if len(m.groups) > 1 {
		parts := make([]string, len(m.groups))
		for i, g := range m.groups {
			parts[i] = fmt.Sprintf("%s %d", g.Type, len(g.Hits))
			if g.Total > len(g.Hits) {
				parts[i] = fmt.Sprintf("%s %d of %d", g.Type, len(g.Hits), g.Total)
			}
		}
		return strings.Join(parts, ", ") + ms
	}
	total := len(m.hits)
	if len(m.groups) == 1 {
		total = max(m.groups[0].Total, total)
	}
	switch n := len(m.hits); {
	case n == 0:
		return "no hits" + ms
	case n == 1:
		return "1 hit" + ms
	case total > n:
		return fmt.Sprintf("%d of %d hits, limit reached%s", n, total, ms)
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

// SetKeys replaces the key bindings, so a changed [keys] table applies
// without a restart; the input and the hits stay as they are.
func (m Model) SetKeys(km keys.Map) tea.Model {
	m.opts.Keys = km
	m.keys = newKeyMap(km)
	return m
}
