// Package memtab is the memory tab of the TUI: the projects that have
// memory in the archive, with the number of their files, and the way into
// the memory viewer (package memview) without a search. Enter, a click on
// the marked row or a second click opens the memory of the marked project;
// a click on another row marks it, the wheel moves the mark.
package memtab

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

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/memview"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// Options configure the tab.
type Options struct {
	// Source reads the projects; nil means there is no database.
	Source project.Source
	// Resolver names the projects like the other views; Home shortens
	// paths in the names.
	Resolver project.Resolver
	Home     string
	// Location is the time zone of the times shown; nil means UTC.
	Location *time.Location
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
}

// Row is a project with memory and its display name.
type Row struct {
	store.MemoryProject
	Name string
}

type loadedMsg struct {
	seq  int
	rows []Row
	err  error
}

// Model is the memory tab.
type Model struct {
	opts Options
	keys KeyMap
	th   theme.Theme

	width, height int

	rows           []Row
	cursor, offset int
	seq            int
	loaded         bool
	err            error
}

// New returns the tab.
func New(o Options) Model {
	if o.Location == nil {
		o.Location = time.UTC
	}
	return Model{opts: o, keys: newKeyMap(o.Keys), th: theme.New(true)}
}

// Title implements tui.View.
func (Model) Title() string { return "memory" }

// Init implements tea.Model: it reads the projects.
func (m Model) Init() tea.Cmd { return m.load() }

// ShortHelp implements help.KeyMap.
func (m Model) ShortHelp() []key.Binding { return m.keys.ShortHelp() }

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// SetKeys replaces the key bindings.
func (m Model) SetKeys(km keys.Map) tea.Model {
	m.opts.Keys, m.keys = km, newKeyMap(km)
	return m
}

// SetProjects applies changed project definitions and reads the names again.
func (m Model) SetProjects(r project.Resolver) (tea.Model, tea.Cmd) {
	m.opts.Resolver = r
	return m.Reload()
}

// Reload reads the projects again; the mark stays on the same project.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	m.seq++
	if m.opts.Source == nil {
		m.err = errors.New("no database")
	}
	return m, m.load()
}

// load reads the projects; the answer counts as current while the model
// still has the sequence number the load was started with.
func (m Model) load() tea.Cmd {
	if m.opts.Source == nil {
		return nil
	}
	seq, o := m.seq, m.opts
	return func() tea.Msg {
		ctx := context.Background()
		cat, err := project.LoadCatalog(ctx, o.Source, o.Resolver, o.Home)
		if err != nil {
			return loadedMsg{seq: seq, err: err}
		}
		ps, err := o.Source.MemoryProjects(ctx)
		if err != nil {
			return loadedMsg{seq: seq, err: err}
		}
		rows := make([]Row, 0, len(ps))
		for _, p := range ps {
			rows = append(rows, Row{MemoryProject: p, Name: cat.MemoryProjectName(p.ID)})
		}
		slices.SortStableFunc(rows, func(a, b Row) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.Harness, b.Harness)
		})
		return loadedMsg{seq: seq, rows: rows}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
	case loadedMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.loaded, m.err = true, msg.err
		if msg.err != nil {
			return m, nil
		}
		var keep int64
		if r := m.marked(); r != nil {
			keep = r.ID
		}
		m.rows, m.cursor = msg.rows, 0
		for i, r := range m.rows {
			if r.ID == keep {
				m.cursor = i
			}
		}
		m.clamp()
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.move(-1)
		case tea.MouseWheelDown:
			m.move(1)
		}
	case tea.MouseClickMsg:
		return m.click(msg)
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.move(1)
	case key.Matches(msg, m.keys.PageUp):
		m.move(-max(m.bodyRows()-1, 1))
	case key.Matches(msg, m.keys.PageDown):
		m.move(max(m.bodyRows()-1, 1))
	case key.Matches(msg, m.keys.Open):
		return m, m.openMarked()
	}
	return m, nil
}

// chromeRows are the head line and the rule above the rows plus the status
// line below.
const chromeRows = 3

func (m Model) bodyRows() int { return max(m.height-chromeRows, 0) }

func (m Model) marked() *Row {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

func (m *Model) move(delta int) {
	m.cursor = min(max(m.cursor+delta, 0), max(len(m.rows)-1, 0))
	m.clamp()
}

// clamp keeps the mark inside the rows shown.
func (m *Model) clamp() {
	rows := m.bodyRows()
	if rows <= 0 {
		return
	}
	m.cursor = min(max(m.cursor, 0), max(len(m.rows)-1, 0))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(min(m.offset, max(len(m.rows)-rows, 0)), 0)
}

// openMarked asks the app to show the memory of the marked project.
func (m Model) openMarked() tea.Cmd {
	r := m.marked()
	if r == nil {
		return nil
	}
	o := memview.Open{ProjectID: r.ID, Project: r.Name, Harness: r.Harness}
	return func() tea.Msg { return o }
}

// click marks the row under the pointer; a click on the marked row opens
// its memory.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	line := msg.Y - (chromeRows - 1)
	i := m.offset + line
	if line < 0 || line >= m.bodyRows() || i >= len(m.rows) {
		return m, nil
	}
	if i == m.cursor {
		return m, m.openMarked()
	}
	m.cursor = i
	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	head := " " + m.th.Title.Render("Memory") + m.th.Muted.Render("  projects with memory")
	lines := []string{ansi.Truncate(head, m.width, "…"), m.th.Rule.Render(strings.Repeat("─", m.width))}
	rows := m.bodyRows()
	body := make([]string, 0, rows)
	for i := m.offset; i < len(m.rows) && len(body) < rows; i++ {
		body = append(body, m.row(i))
	}
	switch {
	case m.err != nil:
		body = append(body, " "+m.th.Error.Render("error: "+m.err.Error()))
	case m.loaded && len(m.rows) == 0:
		body = append(body,
			" "+m.th.Muted.Render("No project has memory in the archive."),
			" "+m.th.Muted.Render("The hooks and `total-recall ingest --all` archive it (ingest.store_memory = true)."))
	}
	for len(body) < rows {
		body = append(body, "")
	}
	lines = append(lines, body[:rows]...)
	lines = append(lines, " "+m.th.Muted.Render(m.status()))
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m Model) status() string {
	files, versions := 0, 0
	for _, r := range m.rows {
		files += r.Files
		versions += r.Versions
	}
	return fmt.Sprintf("%d projects, %d files, %d versions", len(m.rows), files, versions)
}

// row lays out one project: mark, name, harness, files and last change.
func (m Model) row(i int) string {
	r := m.rows[i]
	mark := "  "
	if i == m.cursor {
		mark = m.th.Mark.Render("▸") + " "
	}
	files := fmt.Sprintf("%d files", r.Files)
	if r.Files == 1 {
		files = "1 file"
	}
	if r.Deleted > 0 {
		files += fmt.Sprintf(", %d deleted", r.Deleted)
	}
	last := ""
	if !r.LastChange.IsZero() {
		last = r.LastChange.In(m.opts.Location).Format("2006-01-02 15:04")
	}
	note := fmt.Sprintf("%-8s %-22s %s", r.Harness, files, last)
	room := max(m.width-3-ansi.StringWidth(note)-2, 8)
	name := ansi.Truncate(r.Name, room, "…")
	pad := max(m.width-1-2-ansi.StringWidth(name)-ansi.StringWidth(note)-1, 1)
	text := ansi.Truncate(" "+mark+name+strings.Repeat(" ", pad)+note+" ", m.width, "")
	if i == m.cursor {
		return m.th.Selected.Render(text + strings.Repeat(" ", max(m.width-ansi.StringWidth(text), 0)))
	}
	return text
}
