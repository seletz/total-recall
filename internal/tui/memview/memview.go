// Package memview is the memory view of the TUI: the project memory of one
// project, as the archive holds it (store, schema v8). It is a layer above
// the views like the session detail, opened from a memory hit of the search
// (Open) and closed by Back.
//
// Three pages, esc goes one back:
//
//   - files: the files of the project in the left column, MEMORY.md (the
//     index) first, deleted files last and muted; the marked file's content
//     as readable Markdown on the right (package viewer).
//   - history: the versions of the marked file, newest first, with time,
//     size and the mark of a deletion; on the right the content of the
//     marked version, an earlier state of the file. d switches the right
//     column to the line diff against the version before (or against the
//     version marked with c), added lines green, removed ones red.
//   - timeline: what was added, changed and deleted in the project, newest
//     first, one row per version of any file; on the right the diff of the
//     marked change (the content of an addition).
//
// All data comes from the store's memory API; commands load it with a
// sequence number, so a late answer to an older request is dropped.
package memview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/viewer"
)

// IndexFile is the name of the file that lists the others.
const IndexFile = "MEMORY.md"

// wheelLines is how many lines one wheel turn scrolls the content.
const wheelLines = 3

// Loader reads the memory; *store.Store implements it.
type Loader interface {
	MemoryFiles(ctx context.Context, f store.MemoryFilter) ([]store.MemoryFile, error)
	MemoryVersions(ctx context.Context, projectID int64, file string) ([]store.MemoryVersion, error)
	Blob(ctx context.Context, ref string) (store.BlobInfo, []byte, error)
}

// Open asks the view to show the memory of a project. Path is the file to
// mark, empty for the first one. Terms are highlighted in the content.
type Open struct {
	ProjectID int64
	// Project is the name of the project and Harness its harness, both for
	// the head.
	Project, Harness string
	Path             string
	Terms            []string
}

// Back is sent when the user leaves the view from its first page.
type Back struct{}

type page int

const (
	pageFiles page = iota
	pageHistory
	pageTimeline
)

// Messages of the loads.
type (
	filesMsg struct {
		seq   int
		files []store.MemoryFile
		err   error
	}
	versionsMsg struct {
		seq      int
		path     string
		versions []store.MemoryVersion // oldest first, as the store returns them
		err      error
	}
	timelineMsg struct {
		seq    int
		events []Event
		err    error
	}
	textMsg struct {
		sha  string
		text string
		err  error
	}
)

// Model is the memory view.
type Model struct {
	ld   Loader
	loc  *time.Location
	keys KeyMap
	th   theme.Theme

	width, height int

	open Open
	page page
	// fseq and vseq count the loads of the files and of the versions;
	// an answer to an older one is dropped.
	fseq, vseq int
	err        error

	// files page: the files in display order and the marked one.
	files  []store.MemoryFile
	cursor int
	offset int

	// history page: the versions of histPath, newest first, the marked
	// one. diff shows the diff instead of the content, base is the
	// sequence of the version marked as the one to compare with (0 none).
	// returnTo is the page esc returns to.
	histPath string
	versions []store.MemoryVersion
	vcursor  int
	voffset  int
	diff     bool
	base     int64
	returnTo page

	// timeline page: the changes of the project, newest first.
	events  []Event
	tcursor int
	toffset int
	tseq    int

	// text caches the content by hash; wanted is the hash being loaded.
	text    map[string]string
	loading string

	// scroll is the first line of the content shown, -1 while the view
	// picks it (the first match of the search deep in the file).
	scroll int
	// keep is the path (files) or the version sequence (history) a
	// reload puts the mark back on.
	keepPath string
	keepSeq  int64
}

// New returns the view reading from ld (nil: no database) and showing times
// in loc (nil: UTC), with the key bindings km (nil: the defaults).
func New(ld Loader, loc *time.Location, km keys.Map) Model {
	if loc == nil {
		loc = time.UTC
	}
	return Model{ld: ld, loc: loc, keys: newKeyMap(km), th: theme.New(true), text: map[string]string{}, scroll: -1}
}

// Title implements tui.View.
func (Model) Title() string { return "memory" }

// Init implements tea.Model.
func (Model) Init() tea.Cmd { return nil }

// ShortHelp implements help.KeyMap.
func (m Model) ShortHelp() []key.Binding { return m.keys.ShortHelp(m.page) }

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// SetKeys replaces the key bindings, so a changed [keys] table applies
// without a restart.
func (m Model) SetKeys(km keys.Map) tea.Model {
	m.keys = newKeyMap(km)
	return m
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
	case Open:
		return m.opened(msg)
	case filesMsg:
		return m.filesResult(msg)
	case versionsMsg:
		return m.versionsResult(msg)
	case timelineMsg:
		return m.timelineResult(msg)
	case textMsg:
		return m.textResult(msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseWheelMsg:
		return m.wheel(msg)
	case tea.MouseClickMsg:
		return m.click(msg)
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
	}
	return m, nil
}

// opened starts the view on the files page of a project.
func (m Model) opened(o Open) (tea.Model, tea.Cmd) {
	m.open, m.page = o, pageFiles
	m.files, m.versions, m.events, m.err = nil, nil, nil, nil
	m.cursor, m.offset, m.tcursor, m.toffset, m.scroll = 0, 0, 0, 0, -1
	m.diff, m.base = false, 0
	m.keepPath = o.Path
	return m.loadFiles()
}

// Reload reads the files (and the versions on the history page) again; the
// marks stay on the same file and version.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	if m.ld == nil || m.open.ProjectID == 0 {
		return m, nil
	}
	if f := m.marked(); f != nil {
		m.keepPath = f.Path
	}
	if v := m.markedVersion(); m.page == pageHistory && v != nil {
		m.keepSeq = v.Seq
	}
	if m.page == pageTimeline {
		if e := m.markedEvent(); e != nil {
			m.keepPath, m.keepSeq = e.Path, e.Version.Seq
		}
	}
	m, cmd := m.loadFiles()
	if m.page != pageHistory {
		return m, cmd
	}
	m2, cmd2 := m.loadVersions(m.histPath)
	return m2, tea.Batch(cmd, cmd2)
}

// loadFiles reads the files of the project, deleted ones included.
func (m Model) loadFiles() (Model, tea.Cmd) {
	m.fseq++
	if m.ld == nil {
		m.err = errors.New("no database")
		return m, nil
	}
	seq, ld, pid := m.fseq, m.ld, m.open.ProjectID
	return m, func() tea.Msg {
		files, err := ld.MemoryFiles(context.Background(), store.MemoryFilter{ProjectIDs: []int64{pid}, Deleted: true})
		return filesMsg{seq: seq, files: files, err: err}
	}
}

// loadVersions reads the versions of a file.
func (m Model) loadVersions(path string) (Model, tea.Cmd) {
	m.vseq++
	seq, ld, pid := m.vseq, m.ld, m.open.ProjectID
	return m, func() tea.Msg {
		vs, err := ld.MemoryVersions(context.Background(), pid, path)
		return versionsMsg{seq: seq, path: path, versions: vs, err: err}
	}
}

// order puts the index first, the other files by path, deleted ones last.
func order(files []store.MemoryFile) []store.MemoryFile {
	out := slices.Clone(files)
	rank := func(f store.MemoryFile) int {
		switch {
		case f.Deleted:
			return 2
		case f.Path == IndexFile:
			return 0
		}
		return 1
	}
	slices.SortStableFunc(out, func(a, b store.MemoryFile) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

func (m Model) filesResult(msg filesMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.fseq {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	m.files = order(msg.files)
	m.cursor = 0
	for i, f := range m.files {
		if f.Path == m.keepPath {
			m.cursor = i
		}
	}
	m.clamp()
	if m.page == pageTimeline {
		return m.loadTimeline()
	}
	return m, m.ensureText()
}

func (m Model) versionsResult(msg versionsMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.vseq {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	m.histPath = msg.path
	m.versions = slices.Clone(msg.versions)
	slices.Reverse(m.versions)
	m.vcursor = 0
	for i, v := range m.versions {
		if v.Seq == m.keepSeq {
			m.vcursor = i
		}
	}
	m.keepSeq = 0
	if m.base != 0 && !slices.ContainsFunc(m.versions, func(v store.MemoryVersion) bool { return v.Seq == m.base }) {
		m.base = 0
	}
	m.clamp()
	return m, m.ensureText()
}

func (m Model) timelineResult(msg timelineMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.tseq {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	m.events = msg.events
	m.tcursor = 0
	for i, e := range m.events {
		if e.Version.Seq == m.keepSeq && e.Path == m.keepPath {
			m.tcursor = i
		}
	}
	m.keepSeq = 0
	m.clamp()
	return m, m.ensureText()
}

func (m Model) textResult(msg textMsg) (tea.Model, tea.Cmd) {
	if msg.sha != m.loading {
		return m, nil
	}
	m.loading = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	m.text[msg.sha] = msg.text
	return m, m.ensureText()
}

// marked returns the marked file of the files page, nil without files.
func (m Model) marked() *store.MemoryFile {
	if m.cursor < 0 || m.cursor >= len(m.files) {
		return nil
	}
	return &m.files[m.cursor]
}

// markedVersion returns the marked version of the history page.
func (m Model) markedVersion() *store.MemoryVersion {
	if m.vcursor < 0 || m.vcursor >= len(m.versions) {
		return nil
	}
	return &m.versions[m.vcursor]
}

// wantedSHAs are the hashes of the contents the page shows: one for a
// file or a version, two for a diff (old and new, those that exist).
func (m Model) wantedSHAs() []string {
	var out []string
	add := func(shas ...string) {
		for _, s := range shas {
			if s != "" {
				out = append(out, s)
			}
		}
	}
	switch m.page {
	case pageHistory:
		if v := m.markedVersion(); v != nil && m.diff {
			old, _ := m.compared()
			if old != nil {
				add(old.SHA256)
			}
			add(v.SHA256)
		} else if v != nil {
			add(v.SHA256)
		}
	case pageTimeline:
		if e := m.markedEvent(); e != nil {
			add(e.PrevSHA, e.Version.SHA256)
		}
	default:
		if f := m.marked(); f != nil {
			add(f.SHA256)
		}
	}
	return out
}

// compared returns the version the marked one is compared with on the
// history page: the base when one is set and it is another version, else
// the version before; nil when there is none (the first version). The
// second value tells whether it is the base.
func (m Model) compared() (*store.MemoryVersion, bool) {
	cur := m.markedVersion()
	if cur == nil {
		return nil, false
	}
	if m.base != 0 && m.base != cur.Seq {
		for i := range m.versions {
			if m.versions[i].Seq == m.base {
				return &m.versions[i], true
			}
		}
	}
	if i := m.vcursor + 1; i < len(m.versions) {
		return &m.versions[i], false
	}
	return nil, false
}

// ensureText loads the first content that is shown but neither cached nor
// on its way; its result asks for the next one.
func (m *Model) ensureText() tea.Cmd {
	if m.ld == nil {
		return nil
	}
	for _, sha := range m.wantedSHAs() {
		if _, ok := m.text[sha]; ok {
			continue
		}
		if m.loading == sha {
			return nil
		}
		m.loading = sha
		ld := m.ld
		return func() tea.Msg {
			_, data, err := ld.Blob(context.Background(), sha)
			return textMsg{sha: sha, text: string(data), err: err}
		}
	}
	return nil
}

// clamp keeps the marks and the first visible rows inside the lists.
func (m *Model) clamp() {
	m.cursor = min(max(m.cursor, 0), max(len(m.files)-1, 0))
	m.vcursor = min(max(m.vcursor, 0), max(len(m.versions)-1, 0))
	rows := m.bodyRows()
	m.offset = scrolled(m.offset, m.cursor, rows)
	m.voffset = scrolled(m.voffset, m.vcursor, rows)
	m.tcursor = min(max(m.tcursor, 0), max(len(m.events)-1, 0))
	m.toffset = scrolled(m.toffset, m.tcursor, rows)
}

// scrolled returns the first visible row of a list of rows lines so the
// marked row is in sight.
func scrolled(offset, cursor, rows int) int {
	if rows <= 0 {
		return 0
	}
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+rows {
		return cursor - rows + 1
	}
	return offset
}

// move marks the entry delta away on the current page.
func (m Model) move(delta int) (tea.Model, tea.Cmd) {
	switch m.page {
	case pageHistory:
		m.vcursor += delta
	case pageTimeline:
		m.tcursor += delta
	default:
		m.cursor += delta
	}
	m.scroll = -1
	m.clamp()
	return m, m.ensureText()
}

// enter opens the history of the marked file.
func (m Model) enter() (tea.Model, tea.Cmd) {
	if m.page == pageTimeline {
		return m.openEvent()
	}
	f := m.marked()
	if m.page != pageFiles || f == nil || m.ld == nil {
		return m, nil
	}
	m.returnTo = pageFiles
	return m.openHistory(f.Path, 0)
}

// openHistory shows the history of path with the version of sequence seq
// marked (the newest when 0). The page esc returns to is set by the caller.
func (m Model) openHistory(path string, seq int64) (tea.Model, tea.Cmd) {
	m.page, m.scroll, m.versions, m.vcursor, m.voffset = pageHistory, -1, nil, 0, 0
	m.histPath, m.diff, m.base, m.keepSeq = path, false, 0, seq
	return m.loadVersions(path)
}

// timeline shows what happened in the project.
func (m Model) timeline() (tea.Model, tea.Cmd) {
	if m.ld == nil || m.page == pageTimeline {
		return m, nil
	}
	m.page, m.scroll, m.err = pageTimeline, -1, nil
	if len(m.files) == 0 {
		return m, nil
	}
	return m.loadTimeline()
}

// loadTimeline reads the versions of every file and builds the events.
func (m Model) loadTimeline() (tea.Model, tea.Cmd) {
	m.tseq++
	seq, ld, pid := m.tseq, m.ld, m.open.ProjectID
	paths := make([]string, len(m.files))
	for i, f := range m.files {
		paths[i] = f.Path
	}
	return m, func() tea.Msg {
		versions := make(map[string][]store.MemoryVersion, len(paths))
		for _, p := range paths {
			vs, err := ld.MemoryVersions(context.Background(), pid, p)
			if err != nil {
				return timelineMsg{seq: seq, err: err}
			}
			versions[p] = vs
		}
		return timelineMsg{seq: seq, events: Timeline(versions)}
	}
}

// openEvent opens the history of the marked change's file, on its version.
func (m Model) openEvent() (tea.Model, tea.Cmd) {
	e := m.markedEvent()
	if e == nil {
		return m, nil
	}
	m.returnTo = pageTimeline
	return m.openHistory(e.Path, e.Version.Seq)
}

// markedEvent returns the marked change of the timeline page.
func (m Model) markedEvent() *Event {
	if m.tcursor < 0 || m.tcursor >= len(m.events) {
		return nil
	}
	return &m.events[m.tcursor]
}

// back goes one page back; from the files page the view closes.
func (m Model) back() (tea.Model, tea.Cmd) {
	switch m.page {
	case pageHistory:
		m.page, m.scroll, m.err = m.returnTo, -1, nil
		return m, m.ensureText()
	case pageTimeline:
		m.page, m.scroll, m.err = pageFiles, -1, nil
		return m, m.ensureText()
	}
	return m, func() tea.Msg { return Back{} }
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		return m.back()
	case key.Matches(msg, m.keys.Up):
		return m.move(-1)
	case key.Matches(msg, m.keys.Down):
		return m.move(1)
	case key.Matches(msg, m.keys.Open):
		return m.enter()
	case key.Matches(msg, m.keys.Timeline):
		return m.timeline()
	case key.Matches(msg, m.keys.Diff) && m.page == pageHistory:
		m.diff, m.scroll = !m.diff, -1
		return m, m.ensureText()
	case key.Matches(msg, m.keys.Base) && m.page == pageHistory:
		prev := m.base
		if v := m.markedVersion(); v != nil {
			m.base = v.Seq
			if m.base == prev {
				m.base = 0
			}
		}
		m.scroll = -1
		return m, m.ensureText()
	case key.Matches(msg, m.keys.PageUp):
		m.scrollText(-m.bodyRows() + 1)
	case key.Matches(msg, m.keys.PageDown):
		m.scrollText(m.bodyRows() - 1)
	}
	return m, nil
}

// columns splits the body into the list and the content column, minus the
// separator between them.
func (m Model) columns() (left, right int) {
	left = min(max((m.width-1)*2/5, 24), 48)
	if m.page == pageTimeline {
		// Time, glyph, path and size need room.
		left = min(max((m.width-1)/2, 24), 64)
	}
	left = min(left, max(m.width-1, 0))
	return left, max(m.width-left-1, 0)
}

// Rows around the columns: the head line, a rule and the status line.
const chromeRows = 3

func (m Model) bodyRows() int { return max(m.height-chromeRows, 0) }

// wheel scrolls the content over the right column, moves the mark over the
// list.
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
	if left, _ := m.columns(); msg.X > left {
		m.scrollText(delta * wheelLines)
		return m, nil
	}
	return m.move(delta)
}

// click marks the entry under the pointer; a click on the marked file of
// the files page opens its history.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	line := msg.Y - (chromeRows - 1)
	left, _ := m.columns()
	if line < 0 || line >= m.bodyRows() || msg.X >= left {
		return m, nil
	}
	switch m.page {
	case pageHistory:
		if i := m.voffset + line; i < len(m.versions) && i != m.vcursor {
			m.vcursor, m.scroll = i, -1
			return m, m.ensureText()
		}
		return m, nil
	case pageTimeline:
		switch i := m.toffset + line; {
		case i >= len(m.events):
		case i == m.tcursor:
			return m.openEvent()
		default:
			m.tcursor, m.scroll = i, -1
			return m, m.ensureText()
		}
		return m, nil
	}
	i := m.offset + line
	switch {
	case i >= len(m.files):
		return m, nil
	case i == m.cursor:
		return m.enter()
	}
	m.cursor, m.scroll = i, -1
	return m, m.ensureText()
}

// content renders the viewer of what the page shows, nil when there is
// nothing to show yet (the text is still loading).
func (m Model) content(w int) *viewer.Content {
	env := viewer.Env{Theme: m.th, Location: m.loc, Terms: m.open.Terms}
	var c viewer.Content
	switch m.page {
	case pageHistory:
		ver := m.markedVersion()
		if ver == nil {
			return nil
		}
		note := fmt.Sprintf("version %d of %d", len(m.versions)-m.vcursor, len(m.versions))
		switch {
		case ver.Deleted:
			return m.notice(env, w, m.histPath, ver.TS, note, "The file was deleted at this point.", "An earlier version shows its content.")
		case m.diff:
			old, base := m.compared()
			d := viewer.Diff{Path: m.histPath, Project: m.open.Project, Harness: m.open.Harness, To: ver.TS, ToNote: note}
			if old != nil {
				d.From = old.TS
				d.FromNote = fmt.Sprintf("version %d of %d", len(m.versions)-m.indexOfSeq(old.Seq), len(m.versions))
				if base {
					d.FromNote += " (base)"
				}
				if !old.Deleted {
					d.Old = m.text[old.SHA256]
					if _, ok := m.text[old.SHA256]; !ok {
						return nil
					}
				}
			}
			if _, ok := m.text[ver.SHA256]; !ok {
				return nil
			}
			d.New = m.text[ver.SHA256]
			c = d.Render(env, w)
		default:
			text, ok := m.text[ver.SHA256]
			if !ok {
				return nil
			}
			c = viewer.Memory{Path: m.histPath, Project: m.open.Project, Harness: m.open.Harness, Changed: ver.TS, Note: note, Content: text}.Render(env, w)
		}
	case pageTimeline:
		e := m.markedEvent()
		if e == nil {
			return nil
		}
		switch e.Kind {
		case Deleted:
			return m.notice(env, w, e.Path, e.Version.TS, "", "The file was deleted.", "enter shows its versions.")
		case Added:
			text, ok := m.text[e.Version.SHA256]
			if !ok {
				return nil
			}
			c = viewer.Memory{Path: e.Path, Project: m.open.Project, Harness: m.open.Harness, Changed: e.Version.TS, Note: "added", Content: text}.Render(env, w)
		default:
			old, ok1 := m.text[e.PrevSHA]
			text, ok2 := m.text[e.Version.SHA256]
			if !ok1 || !ok2 {
				return nil
			}
			c = viewer.Diff{Path: e.Path, Project: m.open.Project, Harness: m.open.Harness, From: e.Prev, To: e.Version.TS, Old: old, New: text}.Render(env, w)
		}
	default:
		f := m.marked()
		if f == nil {
			return nil
		}
		if f.Deleted {
			return m.notice(env, w, f.Path, f.Changed, "", "The file was deleted.", "enter shows its versions.")
		}
		text, ok := m.text[f.SHA256]
		if !ok {
			return nil
		}
		c = viewer.Memory{Path: f.Path, Project: m.open.Project, Harness: m.open.Harness, Changed: f.Changed, Content: text}.Render(env, w)
	}
	return &c
}

// notice is the content of a deleted file: the head of the viewer and the
// lines of the note.
func (m Model) notice(env viewer.Env, w int, path string, at time.Time, note string, lines ...string) *viewer.Content {
	c := viewer.Memory{Path: path, Project: m.open.Project, Harness: m.open.Harness, Changed: at, Note: note}.Render(env, w)
	c.Body, c.Anchor = nil, -1
	for _, l := range lines {
		c.Body = append(c.Body, " "+m.th.Muted.Render(l))
	}
	return &c
}

// indexOfSeq returns the index of the version with sequence seq, -1 if it
// is not listed.
func (m Model) indexOfSeq(seq int64) int {
	return slices.IndexFunc(m.versions, func(v store.MemoryVersion) bool { return v.Seq == seq })
}

// textOffset is the first body line shown for avail lines.
func (m Model) textOffset(c viewer.Content, avail int) int {
	limit := max(len(c.Body)-avail, 0)
	if m.scroll >= 0 {
		return min(m.scroll, limit)
	}
	return min(max(c.Anchor-avail/3, 0), limit)
}

// scrollText moves the content by delta lines.
func (m *Model) scrollText(delta int) {
	_, w := m.columns()
	c := m.content(w)
	if c == nil {
		return
	}
	avail := m.bodyRows() - len(c.Head)
	if avail <= 0 || len(c.Body) <= avail {
		return
	}
	m.scroll = min(max(m.textOffset(*c, avail)+delta, 0), len(c.Body)-avail)
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	left, right := m.columns()
	rows := m.bodyRows()
	col := func(s string, w int) string {
		return lipgloss.NewStyle().Width(w).Height(rows).MaxWidth(w).MaxHeight(rows).Render(s)
	}
	sep := m.th.Rule.Render(strings.TrimSuffix(strings.Repeat("│\n", rows), "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, col(m.list(left, rows), left), sep, col(m.contentView(right, rows), right))
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		m.headLine(), m.th.Rule.Render(strings.Repeat("─", m.width)), body, " "+m.status()))
}

// headLine names the project and the page.
func (m Model) headLine() string {
	s := " " + m.th.Title.Render(m.open.Project)
	switch m.page {
	case pageHistory:
		s += m.th.Muted.Render("  history of ") + m.th.AccentText.Render(m.histPath)
		if m.diff {
			s += m.th.Muted.Render("  · ") + m.th.AccentText.Render("diff")
		}
	case pageTimeline:
		s += m.th.Muted.Render("  timeline")
	default:
		s += m.th.Muted.Render("  memory")
		if m.open.Harness != "" {
			s += m.th.Muted.Render("  " + m.open.Harness)
		}
	}
	return ansi.Truncate(s, m.width, "…")
}

// status is the line below the columns: an error, else what the page lists.
func (m Model) status() string {
	switch {
	case m.err != nil:
		return m.th.Error.Render(ansi.Truncate("error: "+m.err.Error(), max(m.width-1, 0), "…"))
	case m.page == pageHistory:
		s := fmt.Sprintf("%d versions", len(m.versions))
		if m.diff && m.base != 0 {
			s += ", compared with the base"
		}
		return m.th.Muted.Render(s)
	case m.page == pageTimeline:
		return m.th.Muted.Render(fmt.Sprintf("%d changes", len(m.events)))
	}
	deleted := 0
	for _, f := range m.files {
		if f.Deleted {
			deleted++
		}
	}
	s := fmt.Sprintf("%d files", len(m.files)-deleted)
	if deleted > 0 {
		s += fmt.Sprintf(", %d deleted", deleted)
	}
	return m.th.Muted.Render(s)
}

// list renders the left column of the current page.
func (m Model) list(w, rows int) string {
	var lines []string
	switch m.page {
	case pageHistory:
		for i := m.voffset; i < len(m.versions) && len(lines) < rows; i++ {
			lines = append(lines, m.versionRow(i, w))
		}
	case pageTimeline:
		for i := m.toffset; i < len(m.events) && len(lines) < rows; i++ {
			lines = append(lines, m.eventRow(i, w))
		}
		if len(m.events) == 0 && m.err == nil {
			lines = append(lines, " "+m.th.Muted.Render("no changes"))
		}
	default:
		for i := m.offset; i < len(m.files) && len(lines) < rows; i++ {
			lines = append(lines, m.fileRow(i, w))
		}
		if len(m.files) == 0 && m.err == nil {
			lines = append(lines, " "+m.th.Muted.Render("no memory files"))
		}
	}
	return strings.Join(lines, "\n")
}

// row lays out a label on the left and a note on the right in w cells; the
// marked row gets the selection background.
func (m Model) row(label, note string, marked, muted bool, w int) string {
	mark := "  "
	if marked {
		mark = m.th.Mark.Render("▸") + " "
	}
	room := max(w-3-ansi.StringWidth(note)-1, 4)
	label = ansi.Truncate(label, room, "…")
	pad := max(w-1-2-ansi.StringWidth(label)-ansi.StringWidth(note)-1, 1)
	text := " " + mark + label + strings.Repeat(" ", pad) + note + " "
	text = ansi.Truncate(text, w, "")
	switch {
	case marked:
		return m.th.Selected.Render(text + strings.Repeat(" ", max(w-ansi.StringWidth(text), 0)))
	case muted:
		return m.th.Muted.Render(text)
	}
	return text
}

func (m Model) fileRow(i, w int) string {
	f := m.files[i]
	label := f.Path
	note := fmt.Sprintf("%d×", f.Versions)
	if f.Deleted {
		label, note = "✗ "+f.Path, "deleted"
	}
	return m.row(label, note, i == m.cursor, f.Deleted, w)
}

func (m Model) versionRow(i, w int) string {
	v := m.versions[i]
	label := v.TS.In(m.loc).Format("2006-01-02 15:04")
	note := formatSize(v.Bytes)
	switch {
	case v.Deleted:
		label, note = "✗ "+label, "deleted"
	case i == 0:
		note += " now"
	}
	if v.Seq == m.base {
		note += " base"
	}
	return m.row(label, note, i == m.vcursor, v.Deleted, w)
}

// formatSize is a size in bytes as B or KiB.
func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KiB", float64(n)/1024)
}

// contentView renders the right column: the head of the viewer and as much
// of the body as fits.
func (m Model) contentView(w, rows int) string {
	if w <= 0 {
		return ""
	}
	c := m.content(w)
	if c == nil {
		if m.err != nil || len(m.wantedSHAs()) == 0 {
			return ""
		}
		return " " + m.th.Muted.Render("loading…")
	}
	if avail := rows - len(c.Head); avail > 0 && len(c.Body) > avail {
		c.Body = c.Body[m.textOffset(*c, avail):]
	}
	return strings.Join(append(c.Head, c.Body...), "\n")
}
