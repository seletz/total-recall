// Package memview is the memory view of the TUI: the project memory of one
// project, as the archive holds it (store, schema v8). It is a layer above
// the views like the session detail, opened from a memory hit of the search
// (Open) and closed by Back.
//
// Two pages, one level each, esc goes one back:
//
//   - files: the files of the project in the left column, MEMORY.md (the
//     index) first, deleted files last and muted; the marked file's content
//     as readable Markdown on the right (package viewer).
//   - history: the versions of the marked file, newest first, with time,
//     size and the mark of a deletion; on the right the content of the
//     marked version, an earlier state of the file.
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
	// one.
	histPath string
	versions []store.MemoryVersion
	vcursor  int
	voffset  int

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
	m.files, m.versions, m.err = nil, nil, nil
	m.cursor, m.offset, m.scroll = 0, 0, -1
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

// wantedSHA is the hash of the content the page shows, "" for a deleted
// file or version and without a mark.
func (m Model) wantedSHA() string {
	if m.page == pageHistory {
		if v := m.markedVersion(); v != nil {
			return v.SHA256
		}
		return ""
	}
	if f := m.marked(); f != nil {
		return f.SHA256
	}
	return ""
}

// ensureText loads the content that is shown unless it is cached or on its
// way.
func (m *Model) ensureText() tea.Cmd {
	sha := m.wantedSHA()
	if sha == "" || m.ld == nil || m.loading == sha {
		return nil
	}
	if _, ok := m.text[sha]; ok {
		return nil
	}
	m.loading = sha
	ld := m.ld
	return func() tea.Msg {
		_, data, err := ld.Blob(context.Background(), sha)
		return textMsg{sha: sha, text: string(data), err: err}
	}
}

// clamp keeps the marks and the first visible rows inside the lists.
func (m *Model) clamp() {
	m.cursor = min(max(m.cursor, 0), max(len(m.files)-1, 0))
	m.vcursor = min(max(m.vcursor, 0), max(len(m.versions)-1, 0))
	rows := m.bodyRows()
	m.offset = scrolled(m.offset, m.cursor, rows)
	m.voffset = scrolled(m.voffset, m.vcursor, rows)
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
	if m.page == pageHistory {
		m.vcursor += delta
	} else {
		m.cursor += delta
	}
	m.scroll = -1
	m.clamp()
	return m, m.ensureText()
}

// enter opens the history of the marked file.
func (m Model) enter() (tea.Model, tea.Cmd) {
	f := m.marked()
	if m.page != pageFiles || f == nil || m.ld == nil {
		return m, nil
	}
	m.page, m.scroll, m.versions, m.vcursor, m.voffset = pageHistory, -1, nil, 0, 0
	m.histPath = f.Path
	return m.loadVersions(f.Path)
}

// back goes one page back; from the files page the view closes.
func (m Model) back() (tea.Model, tea.Cmd) {
	if m.page == pageHistory {
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
	if m.page == pageHistory {
		if i := m.voffset + line; i < len(m.versions) && i != m.vcursor {
			m.vcursor, m.scroll = i, -1
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
// nothing to show yet.
func (m Model) content(w int) *viewer.Content {
	env := viewer.Env{Theme: m.th, Location: m.loc, Terms: m.open.Terms}
	var v viewer.Memory
	switch m.page {
	case pageHistory:
		ver := m.markedVersion()
		if ver == nil {
			return nil
		}
		v = viewer.Memory{Path: m.histPath, Project: m.open.Project, Harness: m.open.Harness, Changed: ver.TS,
			Note: fmt.Sprintf("version %d of %d", len(m.versions)-m.vcursor, len(m.versions))}
		if ver.Deleted {
			c := viewer.Content{Head: v.Render(env, w).Head, Anchor: -1}
			c.Body = []string{" " + m.th.Muted.Render("The file was deleted at this point."), " " + m.th.Muted.Render("An earlier version shows its content.")}
			return &c
		}
		text, ok := m.text[ver.SHA256]
		if !ok {
			return nil
		}
		v.Content = text
	default:
		f := m.marked()
		if f == nil {
			return nil
		}
		v = viewer.Memory{Path: f.Path, Project: m.open.Project, Harness: m.open.Harness, Changed: f.Changed}
		if f.Deleted {
			c := viewer.Content{Head: v.Render(env, w).Head, Anchor: -1}
			c.Body = []string{" " + m.th.Muted.Render("The file was deleted."), " " + m.th.Muted.Render("enter shows its versions.")}
			return &c
		}
		text, ok := m.text[f.SHA256]
		if !ok {
			return nil
		}
		v.Content = text
	}
	c := v.Render(env, w)
	return &c
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
	if m.page == pageHistory {
		s += m.th.Muted.Render("  history of ") + m.th.AccentText.Render(m.histPath)
	} else {
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
		return m.th.Muted.Render(fmt.Sprintf("%d versions", len(m.versions)))
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
	if m.page == pageHistory {
		for i := m.voffset; i < len(m.versions) && len(lines) < rows; i++ {
			lines = append(lines, m.versionRow(i, w))
		}
	} else {
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
		if m.wantedSHA() == "" && m.err == nil && (m.page == pageHistory && m.markedVersion() == nil || m.page == pageFiles && m.marked() == nil) {
			return ""
		}
		return " " + m.th.Muted.Render("loading…")
	}
	if avail := rows - len(c.Head); avail > 0 && len(c.Body) > avail {
		c.Body = c.Body[m.textOffset(*c, avail):]
	}
	return strings.Join(append(c.Head, c.Body...), "\n")
}
