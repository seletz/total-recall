package search

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/session"
)

// pin is the project the search is restricted to: its display name and
// the scope the store applies to every type.
type pin struct {
	name  string
	scope *store.ProjectScope
}

// catalogMsg is the answer of loading the project catalog.
type catalogMsg struct {
	cat *project.Catalog
	err error
}

// loadCatalog reads the project catalog in the background; without a
// project source the view works without names and without a project list.
func (m Model) loadCatalog() tea.Cmd {
	src, res, home := m.opts.Projects, m.opts.Resolver, m.opts.Home
	if src == nil {
		return nil
	}
	return func() tea.Msg {
		cat, err := project.LoadCatalog(context.Background(), src, res, home)
		return catalogMsg{cat: cat, err: err}
	}
}

// projectName is the display name of the project of hit h, "" when it is
// unknown.
func (m Model) projectName(h store.Hit) string {
	switch {
	case m.cat != nil && h.Memory != nil:
		return m.cat.MemoryName(h.Memory)
	case m.cat != nil:
		if n := m.cat.Name(h.Project); n != "" {
			return n
		}
	case h.Memory != nil:
		return memoryProject(h)
	}
	return session.ShortRepo(h.Project.Key)
}

// pinOf is the pin for the project of hit h.
func (m Model) pinOf(h store.Hit) *pin {
	var id int64
	if h.Memory != nil {
		id = h.Memory.ProjectID
	}
	if m.cat != nil {
		name, sc := m.cat.ScopeOf(h.Project, id)
		if name != "" {
			return &pin{name: name, scope: sc}
		}
		return nil
	}
	sc := &store.ProjectScope{Keys: []string{}, MemoryProjectIDs: []int64{}}
	if h.Project.Key != "" {
		sc.Keys = append(sc.Keys, h.Project.Key)
	}
	if id != 0 {
		sc.MemoryProjectIDs = append(sc.MemoryProjectIDs, id)
	}
	if len(sc.Keys) == 0 && len(sc.MemoryProjectIDs) == 0 {
		return nil
	}
	return &pin{name: m.projectName(h), scope: sc}
}

// togglePin drops the pin, or pins the project of the marked hit.
func (m Model) togglePin() (tea.Model, tea.Cmd) {
	if m.pin != nil {
		m.pin = nil
		return m.refilter()
	}
	h := m.marked()
	if h == nil {
		return m, nil
	}
	if p := m.pinOf(*h); p != nil {
		m.pin = p
		return m.refilter()
	}
	return m, nil
}

// picker is the state of the project choice: the entries (the first one
// is "all projects") and the mark.
type picker struct {
	entries []project.Entry
	cursor  int
	offset  int
}

// openPicker shows the list of projects in the list column, the mark on
// the pinned one.
func (m Model) openPicker() Model {
	entries := []project.Entry{{Name: "all projects"}}
	if m.cat != nil {
		entries = append(entries, m.cat.Entries()...)
	}
	m.pick = picker{entries: entries}
	for i, e := range entries {
		if m.pin != nil && e.Name == m.pin.name {
			m.pick.cursor = i
		}
	}
	m.pick.scroll(m.bodyRows())
	m.focus = focusPick
	m.input.Blur()
	return m
}

// scroll keeps the mark in the rows visible.
func (p *picker) scroll(rows int) {
	if rows <= 0 {
		return
	}
	p.offset = min(max(p.offset, p.cursor-rows+1), p.cursor)
	p.offset = max(min(p.offset, len(p.entries)-rows), 0)
}

// pickKey handles the keys of the project choice: up and down move,
// the open key applies, the back key (esc) leaves it as it was.
func (m Model) pickKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.pick.cursor = max(m.pick.cursor-1, 0)
		m.pick.scroll(m.bodyRows())
	case key.Matches(msg, m.keys.Down):
		m.pick.cursor = min(m.pick.cursor+1, len(m.pick.entries)-1)
		m.pick.scroll(m.bodyRows())
	case key.Matches(msg, m.keys.Confirm):
		return m.applyPick(m.pick.cursor)
	case key.Matches(msg, m.keys.Cancel):
		m.focus = focusList
	}
	return m, nil
}

// applyPick pins the project of entry i ("all projects" drops the pin)
// and searches again.
func (m Model) applyPick(i int) (tea.Model, tea.Cmd) {
	m.focus = focusList
	if i < 0 || i >= len(m.pick.entries) {
		return m, nil
	}
	if e := m.pick.entries[i]; e.Scope == nil {
		m.pin = nil
	} else {
		m.pin = &pin{name: e.Name, scope: e.Scope}
	}
	return m.refilter()
}

// pickView renders the project choice in the list column.
func (m Model) pickView(w, rows int) string {
	lines := make([]string, 0, rows)
	for i := m.pick.offset; i < len(m.pick.entries) && i < m.pick.offset+rows; i++ {
		mark, text := " ", m.pick.entries[i].Name
		st := m.th.Muted
		if i == m.pick.cursor {
			mark, st = "▸", m.th.Selected
		}
		lines = append(lines, m.th.Mark.Render(mark)+st.Render(strings.TrimRight(ansi.Truncate(" "+text, max(w-1, 0), "…"), " ")))
	}
	return strings.Join(lines, "\n")
}
