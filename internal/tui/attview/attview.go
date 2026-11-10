// Package attview is the attachment view of the TUI: the images and
// documents of one event, as the archive holds them (store, schema v5 and
// up: blobs linked to events). It is a layer above the views like the
// session detail and the memory view, opened from an event with
// attachments (Open) and closed by Back.
//
// A strip below the head lists the attachments of the event, one tab
// each; the keys (and a click) browse them. The marked one is shown by
// viewer.Attachment: an image as text (package imgtext), a PDF with its
// metadata, a text from its start, anything else as metadata; every kind
// names the command that writes the data to a file. The open key writes
// the data to a private temporary file and hands it to the program of the
// system (External).
//
// All data comes from the store; commands load it with a sequence number,
// so a late answer to an older request is dropped.
package attview

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/docmeta"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/viewer"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// wheelLines is how many lines one wheel turn scrolls the content.
const wheelLines = 3

// Rows around the content: the head, the tab strip and the status line.
const chromeRows = 3

// Loader reads the attachments; *store.Store implements it.
type Loader interface {
	SessionAttachmentList(ctx context.Context, sessionID string) ([]store.SessionAttachment, error)
	Blob(ctx context.Context, ref string) (store.BlobInfo, []byte, error)
}

// Options configure the view.
type Options struct {
	// Loader reads the attachments; nil means no database.
	Loader Loader
	// Location is the time zone; nil means UTC.
	Location *time.Location
	// External opens an attachment with the system's program; nil
	// disables the open key (the status line says so).
	External External
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
}

// Open asks the view to show the attachments of a session, with the first
// attachment of the event EventID marked (the first of the session when
// the event has none, or EventID is empty). Title names the session for
// the head.
type Open struct {
	SessionID string
	EventID   string
	Title     string
}

// Back is sent when the user leaves the view.
type Back struct{}

// Messages of the loads.
type (
	loadedMsg struct {
		seq  int
		atts []store.SessionAttachment
		err  error
	}
	blobMsg struct {
		sha  string
		info store.BlobInfo
		data []byte
		err  error
	}
)

// entry is a loaded attachment; data is nil when the archive holds none.
type entry struct {
	info store.BlobInfo
	data []byte
}

// Model is the attachment view.
type Model struct {
	opts Options
	keys KeyMap
	th   theme.Theme

	width, height int

	open Open
	seq  int
	err  error
	atts []store.SessionAttachment
	cur  int
	// want is the event whose first attachment the next answer marks.
	want string
	// blobs caches the loaded data by hash; loading is the hash on its way.
	blobs   map[string]entry
	loading string
	// scroll is the first line of the body shown.
	scroll int
	// status is the message below the content (what the open key did).
	status string
}

// New returns the view.
func New(opts Options) Model {
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	return Model{opts: opts, keys: newKeyMap(opts.Keys), th: theme.New(true), blobs: map[string]entry{}}
}

// Title implements tui.View.
func (Model) Title() string { return "attachments" }

// Init implements tea.Model.
func (Model) Init() tea.Cmd { return nil }

// ShortHelp implements help.KeyMap.
func (m Model) ShortHelp() []key.Binding { return m.keys.ShortHelp() }

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// SetKeys replaces the key bindings, so a changed [keys] table applies
// without a restart.
func (m Model) SetKeys(km keys.Map) tea.Model {
	m.opts.Keys = km
	m.keys = newKeyMap(km)
	return m
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case Open:
		return m.opened(msg)
	case loadedMsg:
		return m.loadedResult(msg)
	case blobMsg:
		return m.blobResult(msg)
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

// opened starts the view on the attachments of an event.
func (m Model) opened(o Open) (tea.Model, tea.Cmd) {
	m.open = o
	m.atts, m.err, m.cur, m.scroll, m.status, m.want = nil, nil, 0, 0, "", o.EventID
	return m.load()
}

// Reload reads the attachments again; the mark stays on the same one.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	if m.open.SessionID == "" {
		return m, nil
	}
	return m.load()
}

// load reads the attachments of the event.
func (m Model) load() (tea.Model, tea.Cmd) {
	m.seq++
	if m.opts.Loader == nil {
		m.err = errors.New("no database")
		return m, nil
	}
	seq, ld, id := m.seq, m.opts.Loader, m.open.SessionID
	return m, func() tea.Msg {
		atts, err := ld.SessionAttachmentList(context.Background(), id)
		return loadedMsg{seq: seq, atts: atts, err: err}
	}
}

func (m Model) loadedResult(msg loadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.seq {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	var keep string // a reload keeps the mark on the same attachment
	if a := m.marked(); a != nil {
		keep = a.SHA256
	}
	m.atts = msg.atts
	m.cur = min(m.cur, max(len(m.atts)-1, 0))
	for i, a := range m.atts {
		if m.want != "" && a.EventID == m.want || m.want == "" && a.SHA256 == keep {
			m.cur = i
			break
		}
	}
	m.want = ""
	return m, m.ensureBlob()
}

func (m Model) blobResult(msg blobMsg) (tea.Model, tea.Cmd) {
	if msg.sha != m.loading {
		return m, nil
	}
	m.loading = ""
	switch {
	case errors.Is(msg.err, store.ErrNotFound):
		// The link is there, the data is not (store_blobs = false).
		m.blobs[msg.sha] = entry{}
	case msg.err != nil:
		m.err = msg.err
		return m, nil
	default:
		m.err = nil
		m.blobs[msg.sha] = entry{info: msg.info, data: msg.data}
	}
	return m, m.ensureBlob()
}

// marked returns the marked attachment, nil without any.
func (m Model) marked() *store.SessionAttachment {
	if m.cur < 0 || m.cur >= len(m.atts) {
		return nil
	}
	return &m.atts[m.cur]
}

// ensureBlob loads the data of the marked attachment unless it is cached
// or on its way.
func (m *Model) ensureBlob() tea.Cmd {
	a := m.marked()
	if a == nil || m.opts.Loader == nil {
		return nil
	}
	if _, ok := m.blobs[a.SHA256]; ok || m.loading == a.SHA256 {
		return nil
	}
	m.loading = a.SHA256
	ld, sha := m.opts.Loader, a.SHA256
	return func() tea.Msg {
		info, data, err := ld.Blob(context.Background(), sha)
		return blobMsg{sha: sha, info: info, data: data, err: err}
	}
}

// pick marks attachment i and loads its data.
func (m Model) pick(i int) (tea.Model, tea.Cmd) {
	i = min(max(i, 0), max(len(m.atts)-1, 0))
	if i != m.cur {
		m.cur, m.scroll, m.status = i, 0, ""
	}
	return m, m.ensureBlob()
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		return m, func() tea.Msg { return Back{} }
	case key.Matches(msg, m.keys.Prev):
		return m.pick(m.cur - 1)
	case key.Matches(msg, m.keys.Next):
		return m.pick(m.cur + 1)
	case key.Matches(msg, m.keys.Up):
		m.scrollBy(-1)
	case key.Matches(msg, m.keys.Down):
		m.scrollBy(1)
	case key.Matches(msg, m.keys.PageUp):
		m.scrollBy(-m.contentRows() + 1)
	case key.Matches(msg, m.keys.PageDown):
		m.scrollBy(m.contentRows() - 1)
	case key.Matches(msg, m.keys.Open):
		return m.external()
	}
	return m, nil
}

// external hands the marked attachment to the program of the system.
func (m Model) external() (tea.Model, tea.Cmd) {
	a := m.marked()
	if a == nil {
		return m, nil
	}
	hint := "total-recall blob " + short(a.SHA256) + " --out FILE"
	e, ok := m.blobs[a.SHA256]
	switch {
	case m.opts.External == nil:
		m.status = "cannot open: no program to open files; " + hint
	case !ok:
		m.status = "still loading…"
	case e.data == nil:
		m.status = "cannot open: data not stored; " + hint
	default:
		path, err := m.opts.External.Open(short(a.SHA256)+docmeta.Ext(a.MediaType), e.data)
		if err != nil {
			m.status = "cannot open: " + err.Error() + "; " + hint
		} else {
			m.status = "opened " + path
		}
	}
	return m, nil
}

func short(sha string) string { return sha[:min(len(sha), 12)] }

// contentRows is the height of the viewer's column: the view without
// head, tab strip and status line.
func (m Model) contentRows() int { return max(m.height-chromeRows, 0) }

// content renders the marked attachment in the room under the strip.
func (m Model) content() viewer.Content {
	a := m.marked()
	if a == nil {
		return viewer.Content{}
	}
	v := viewer.Attachment{Info: a.BlobInfo, Index: m.cur + 1, Count: len(m.atts)}
	if e, ok := m.blobs[a.SHA256]; ok {
		v.Data = e.data
	} else {
		return viewer.Content{Body: []string{" " + m.th.Muted.Render("loading…")}}
	}
	return v.Render(viewer.Env{Theme: m.th, Location: m.opts.Location, Rows: m.contentRows()}, m.width)
}

// scrollBy moves the body by delta lines, within it.
func (m *Model) scrollBy(delta int) {
	c := m.content()
	room := max(m.contentRows()-len(c.Head), 0)
	m.scroll = min(max(m.scroll+delta, 0), max(len(c.Body)-room, 0))
}

func (m Model) wheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.scrollBy(-wheelLines)
	case tea.MouseWheelDown:
		m.scrollBy(wheelLines)
	}
	return m, nil
}

// click marks the attachment whose tab is under the pointer.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || msg.Y != 1 {
		return m, nil
	}
	_, spans := m.strip()
	if i := zone.At(spans, msg.X); i >= 0 {
		return m.pick(i)
	}
	return m, nil
}

// strip renders the tab strip, one tab per attachment ("2 application/pdf
// 1.2 KiB", the marked one on the active style), cut to the width with
// the marked one in sight, and the span of each tab for clicks (an empty
// span for a tab out of sight).
func (m Model) strip() (string, []zone.Span) {
	w := m.width
	type tab struct {
		text string
		w    int
	}
	tabs := make([]tab, len(m.atts))
	for i, a := range m.atts {
		t := strconv.Itoa(i+1) + " " + viewer.AttachmentNote(a.EventAttachment)
		if i == m.cur {
			t = "[" + t + "]"
		}
		tabs[i] = tab{t, ansi.StringWidth(t)}
	}
	// Start at the first tab such that the marked one still fits.
	start := 0
	for start < m.cur {
		used := 1
		for j := start; j <= m.cur; j++ {
			used += tabs[j].w + 1
		}
		if used <= w {
			break
		}
		start++
	}
	spans := make([]zone.Span, len(tabs))
	var b strings.Builder
	b.WriteString(" ")
	x := 1
	for i := start; i < len(tabs); i++ {
		if x+tabs[i].w > w {
			if i == m.cur { // narrower than the marked tab: cut it
				t := ansi.Truncate(tabs[i].text, max(w-x, 0), "…")
				b.WriteString(m.th.Active.Render(t))
				spans[i] = zone.Span{Start: x, End: w}
			}
			break
		}
		if i == m.cur {
			b.WriteString(m.th.Active.Render(tabs[i].text))
		} else {
			b.WriteString(m.th.Muted.Render(tabs[i].text))
		}
		spans[i] = zone.Span{Start: x, End: x + tabs[i].w}
		b.WriteString(" ")
		x += tabs[i].w + 1
	}
	return ansi.Truncate(b.String(), w, ""), spans
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	rows := m.contentRows()
	var body []string
	switch {
	case m.err != nil:
		body = []string{" " + m.th.Error.Render(ansi.Truncate("error: "+m.err.Error(), max(m.width-1, 0), "…"))}
	case len(m.atts) == 0 && m.seq > 0 && m.loading == "":
		body = []string{" " + m.th.Muted.Render("no attachments")}
	default:
		c := m.content()
		body = append(body, c.Head...)
		off := min(m.scroll, max(len(c.Body)-max(rows-len(c.Head), 0), 0))
		body = append(body, c.Body[off:]...)
	}
	if len(body) > rows {
		body = body[:rows]
	}
	strip, _ := m.strip()
	title := " " + m.th.Title.Render(m.open.Title) + m.th.Muted.Render("  attachments")
	lines := []string{ansi.Truncate(title, m.width, "…"), strip}
	lines = append(lines, body...)
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines[:min(len(lines), max(m.height-1, 0))], m.statusLine())
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "")
	}
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, lines[:min(len(lines), m.height)]...))
}

// statusLine is what the open key did, else what the view shows.
func (m Model) statusLine() string {
	if m.status != "" {
		return " " + m.th.Muted.Render(ansi.Truncate(m.status, max(m.width-1, 0), "…"))
	}
	n := len(m.atts)
	if n == 0 {
		return ""
	}
	return " " + m.th.Muted.Render(fmt.Sprintf("%d attachments", n))
}
