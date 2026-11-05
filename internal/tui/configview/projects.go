package configview

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// pmode is what the projects page is doing.
type pmode int

const (
	// modeList shows the definitions and the unmatched paths of the archive.
	modeList pmode = iota
	// modeForm edits one definition (a draft, written with confirm).
	modeForm
	// modeDelete waits for the confirmation of a removal.
	modeDelete
	// modePick chooses the definition that gets a path as a pattern.
	modePick
)

// prowKind is the kind of a row of the list.
type prowKind int

const (
	rowDef prowKind = iota
	rowHeader
	rowKey
)

// prow is one row of the list: a definition, a header line or a path of
// the archive without a definition.
type prow struct {
	kind  prowKind
	index int // into the definitions or the archive keys
	label string
}

// keysMsg carries the project keys of the archive.
type keysMsg struct {
	keys []store.ProjectKey
	err  error
}

// projectsPage lists the project definitions ([[project]]) in the order of
// the file (the first match wins) with what each one gets of the archive,
// and below them the paths of the archive that no definition matches.
// Definitions are added, edited (a form with a draft, written as a whole),
// removed after a confirmation and moved; a path becomes a new definition
// or a pattern of an existing one. Every write goes through the
// configuration (config.AddProject and friends), so the file keeps its
// comments, a backup is made and a rejected definition leaves it as it is.
type projectsPage struct {
	mode   pmode
	rows   []prow
	row    int
	offset int
	ar     archive
	form   form
	// pending is the pattern waiting for its definition in modePick.
	pending string
	// want is the name to mark after the next build (a saved
	// definition).
	want   string
	notice string
	bad    bool
}

// form is the draft of one definition being edited.
type form struct {
	// index is the entry being edited, -1 for a new one.
	index int
	draft config.Project
	// field is the marked field: name, description, colour, the patterns,
	// then the add-pattern row.
	field   int
	editing bool
	input   textinput.Model
	// picking is the colour picker, pick its marked choice.
	picking bool
	pick    int
	dirty   bool
	// warned is set after esc with unsaved changes; the next esc leaves.
	warned bool
}

// Fields of the form before the patterns.
const (
	fieldName = iota
	fieldDescription
	fieldColor
	fieldPatterns
)

func newProjectsPage() projectsPage {
	in := textinput.New()
	in.Prompt = "> "
	in.SetVirtualCursor(false)
	return projectsPage{form: form{input: in}}
}

func (projectsPage) title() string { return "projects" }

func (p projectsPage) focused() bool { return p.mode != modeList }

func (p projectsPage) shortHelp(m *Model) []key.Binding {
	switch p.mode {
	case modeForm:
		if p.form.editing || p.form.picking {
			return []key.Binding{m.keys.Select, m.keys.Back}
		}
		return []key.Binding{m.keys.Select, m.keys.Add, m.keys.Remove, m.keys.Confirm, m.keys.Back}
	case modeDelete:
		return []key.Binding{m.keys.Confirm, m.keys.Back}
	case modePick:
		return []key.Binding{m.keys.Move, m.keys.Select, m.keys.Back}
	}
	return []key.Binding{m.keys.Move, m.keys.Select, m.keys.Add, m.keys.Remove, m.keys.Pattern, m.keys.Editor}
}

func (p projectsPage) fullHelp(m *Model) [][]key.Binding {
	if p.mode == modeForm {
		return [][]key.Binding{{m.keys.Up, m.keys.Down, m.keys.Select, m.keys.Add}, {m.keys.Remove, m.keys.Default, m.keys.Confirm, m.keys.Back}}
	}
	return [][]key.Binding{{m.keys.Up, m.keys.Down, m.keys.Select, m.keys.Add, m.keys.Remove}, {m.keys.MoveUp, m.keys.MoveDn, m.keys.Pattern, m.keys.Editor, m.keys.Back}}
}

// reload loads the project keys of the archive again.
func (p projectsPage) reload(m *Model) (page, tea.Cmd) {
	p = p.build(m)
	st := m.opts.Store
	if st == nil {
		return p, nil
	}
	return p, func() tea.Msg {
		keys, err := st.ProjectKeys(context.Background())
		return keysMsg{keys: keys, err: err}
	}
}

// build lays the rows out from the configuration and the archive, keeping
// the mark on the same definition.
func (p projectsPage) build(m *Model) projectsPage {
	cfg := m.opts.Config
	if cfg == nil {
		p.rows = nil
		return p
	}
	name := p.want
	p.want = ""
	if r := p.current(); name == "" && r.kind == rowDef && r.index < len(cfg.Projects) {
		name = cfg.Projects[r.index].Name
	}
	var rows []prow
	for i := range cfg.Projects {
		rows = append(rows, prow{kind: rowDef, index: i})
	}
	if p.ar.loaded {
		r, _ := pageResolver(cfg)
		if un := assign(r, p.ar).unmatched; len(un) > 0 {
			rows = append(rows, prow{kind: rowHeader}, prow{kind: rowHeader, label: "UNMATCHED"})
			for _, i := range un {
				rows = append(rows, prow{kind: rowKey, index: i})
			}
		}
	}
	p.rows = rows
	p.row = min(p.row, max(len(rows)-1, 0))
	if name != "" {
		for i, r := range rows {
			if r.kind == rowDef && cfg.Projects[r.index].Name == name {
				p.row = i
			}
		}
	}
	if p.row < len(p.rows) && p.rows[p.row].kind == rowHeader {
		p.row = p.next(p.row, 1)
	}
	return p
}

// current is the marked row (a header without rows).
func (p projectsPage) current() prow {
	if p.row < 0 || p.row >= len(p.rows) {
		return prow{kind: rowHeader}
	}
	return p.rows[p.row]
}

// next returns the index of the first row that is no header after from in
// direction dir (in modePick: the next definition), or from.
func (p projectsPage) next(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(p.rows); i += dir {
		if p.rows[i].kind == rowDef || p.rows[i].kind == rowKey && p.mode != modePick {
			return i
		}
	}
	return from
}

func (p projectsPage) move(dir int) projectsPage {
	if n := p.next(p.row, dir); n != p.row {
		p.row = n
	}
	p.notice = ""
	return p
}

func (p projectsPage) update(m *Model, msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case keysMsg:
		if msg.err != nil {
			p.ar = archive{loaded: true, err: msg.err}
		} else {
			p.ar = newArchive(msg.keys, m.opts.Home)
		}
		return p.build(m), nil
	case Changed:
		return p.build(m), nil
	case editedMsg:
		var cmd tea.Cmd
		p.notice, p.bad, cmd = m.reloadFile(msg)
		return p, cmd
	case tea.KeyPressMsg:
		switch p.mode {
		case modeForm:
			return p.formKey(m, msg)
		case modeDelete:
			return p.deleteKey(m, msg)
		case modePick:
			return p.pickKey(m, msg)
		}
		return p.listKey(m, msg)
	case tea.MouseClickMsg:
		return p.click(m, msg)
	case tea.MouseWheelMsg:
		if p.mode == modeForm {
			return p, nil
		}
		if msg.Button == tea.MouseWheelUp {
			return p.move(-1), nil
		}
		return p.move(1), nil
	}
	return p, nil
}

// ---- list ----

func (p projectsPage) listKey(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	if m.opts.Config == nil {
		return p, nil
	}
	r := p.current()
	switch {
	case key.Matches(msg, m.keys.Up):
		return p.move(-1), nil
	case key.Matches(msg, m.keys.Down):
		return p.move(1), nil
	case key.Matches(msg, m.keys.Select):
		return p.open(m), nil
	case key.Matches(msg, m.keys.Add):
		if r.kind == rowKey {
			return p.open(m), nil
		}
		return p.edit(-1, config.Project{}), nil
	case key.Matches(msg, m.keys.Remove):
		if r.kind != rowDef {
			return p, nil
		}
		p.mode, p.notice, p.bad = modeDelete, "", false
		return p, nil
	case key.Matches(msg, m.keys.MoveUp):
		return p.reorder(m, -1)
	case key.Matches(msg, m.keys.MoveDn):
		return p.reorder(m, 1)
	case key.Matches(msg, m.keys.Pattern):
		return p.startPick(m), nil
	case key.Matches(msg, m.keys.Editor):
		notice, cmd := m.editor()
		if cmd == nil {
			p.notice, p.bad = notice, true
		}
		return p, cmd
	case key.Matches(msg, m.keys.Back):
		p.notice = ""
	}
	return p, nil
}

// open edits the marked definition, or starts a new one from the marked
// path of the archive.
func (p projectsPage) open(m *Model) projectsPage {
	r := p.current()
	switch r.kind {
	case rowDef:
		return p.edit(r.index, m.opts.Config.Projects[r.index])
	case rowKey:
		return p.edit(-1, suggest(p.ar.keys[r.index], p.ar, m.opts.Home))
	}
	return p
}

// edit opens the form for the index-th entry (-1: new) with draft.
func (p projectsPage) edit(index int, draft config.Project) projectsPage {
	draft.Match = append(config.StringList{}, draft.Match...)
	p.mode = modeForm
	p.form = form{index: index, draft: draft, input: p.form.input, dirty: index < 0}
	p.form.input.Blur()
	p.notice, p.bad = "", false
	if index < 0 && draft.Name == "" {
		// A fresh definition: start typing the name right away.
		p = p.startInput(fieldName)
	}
	return p
}

// reorder moves the marked definition one up or down in the file.
func (p projectsPage) reorder(m *Model, dir int) (page, tea.Cmd) {
	r := p.current()
	n := len(m.opts.Config.Projects)
	if r.kind != rowDef || r.index+dir < 0 || r.index+dir >= n {
		return p, nil
	}
	cfg, err := m.opts.Config.MoveProject(r.index, r.index+dir)
	if err != nil {
		p.notice, p.bad = err.Error(), true
		return p, nil
	}
	p.want = cfg.Projects[r.index+dir].Name
	dirName := "down"
	if dir < 0 {
		dirName = "up"
	}
	p.notice, p.bad = fmt.Sprintf("%s moved %s · first match wins", p.want, dirName)+applied, false
	return p, changed(cfg)
}

// startPick takes the marked path of the archive as a pattern and asks
// which definition gets it.
func (p projectsPage) startPick(m *Model) projectsPage {
	r := p.current()
	if r.kind != rowKey {
		p.notice, p.bad = "mark a path under UNMATCHED to add it as a pattern", true
		return p
	}
	if len(m.opts.Config.Projects) == 0 {
		p.notice, p.bad = "no definition to add the pattern to; "+m.keys.Add.Help().Key+" creates one", true
		return p
	}
	p.pending = suggestPattern(p.ar.keys[r.index], m.opts.Home)
	p.mode = modePick
	p.row = p.next(-1, 1)
	p.notice, p.bad = "", false
	return p
}

func (p projectsPage) pickKey(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		return p.move(-1), nil
	case key.Matches(msg, m.keys.Down):
		return p.move(1), nil
	case key.Matches(msg, m.keys.Select):
		r := p.current()
		if r.kind != rowDef {
			return p, nil
		}
		draft := m.opts.Config.Projects[r.index]
		draft.Match = append(append(config.StringList{}, draft.Match...), p.pending)
		cfg, err := m.opts.Config.SetProject(r.index, draft)
		if err != nil {
			p.notice, p.bad = err.Error(), true
			return p, nil
		}
		p.mode = modeList
		p.notice, p.bad = fmt.Sprintf("%q added to %s", p.pending, draft.Name)+applied, false
		return p, changed(cfg)
	case key.Matches(msg, m.keys.Back):
		p.mode = modeList
	}
	return p, nil
}

func (p projectsPage) deleteKey(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Confirm):
		r := p.current()
		name := m.opts.Config.Projects[r.index].Name
		cfg, err := m.opts.Config.RemoveProject(r.index)
		p.mode = modeList
		if err != nil {
			p.notice, p.bad = err.Error(), true
			return p, nil
		}
		p.notice, p.bad = name+" removed from "+m.tilde(cfg.Path)+applied, false
		return p, changed(cfg)
	case key.Matches(msg, m.keys.Back):
		p.mode = modeList
	}
	return p, nil
}

// applied says that a change shows in the other views at once.
const applied = " · stats and activity reload"

// changed hands the new configuration to the app.
func changed(cfg *config.Config) tea.Cmd {
	return func() tea.Msg { return Changed{Config: cfg} }
}

// ---- form ----

// nFields is the number of rows of the form: the fixed fields, the
// patterns and the add row.
func (f form) nFields() int { return fieldPatterns + len(f.draft.Match) + 1 }

// pattern reports whether field i is a pattern.
func (f form) pattern(i int) bool { return i >= fieldPatterns && i < fieldPatterns+len(f.draft.Match) }

func (p projectsPage) formKey(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.form.editing:
		return p.inputKey(m, msg)
	case p.form.picking:
		return p.pickerKey(m, msg), nil
	}
	f := &p.form
	switch {
	case key.Matches(msg, m.keys.Up):
		f.field = max(f.field-1, 0)
		p.notice = ""
	case key.Matches(msg, m.keys.Down):
		f.field = min(f.field+1, f.nFields()-1)
		p.notice = ""
	case key.Matches(msg, m.keys.Select):
		return p.activate(), nil
	case key.Matches(msg, m.keys.Add):
		return p.addPattern(), nil
	case key.Matches(msg, m.keys.Remove):
		if f.pattern(f.field) {
			i := f.field - fieldPatterns
			f.draft.Match = append(f.draft.Match[:i:i], f.draft.Match[i+1:]...)
			f.field = min(f.field, f.nFields()-1)
			f.dirty = true
		}
	case key.Matches(msg, m.keys.Default):
		switch f.field {
		case fieldDescription:
			f.draft.Description = ""
			f.dirty = true
		case fieldColor:
			f.draft.Color = ""
			f.dirty = true
		}
	case key.Matches(msg, m.keys.Confirm):
		return p.save(m)
	case key.Matches(msg, m.keys.Back):
		if f.dirty && !f.warned {
			f.warned = true
			p.notice, p.bad = "unsaved changes: "+m.keys.Confirm.Help().Key+" saves, "+m.keys.Back.Help().Key+" again leaves", true
			return p, nil
		}
		p.mode = modeList
		p.notice, p.bad = "", false
	}
	return p, nil
}

// activate acts on the marked field: a text field opens the input, the
// colour opens the picker, the add row adds a pattern.
func (p projectsPage) activate() projectsPage {
	f := p.form
	switch {
	case f.field == fieldColor:
		p.form.picking = true
		p.form.pick = colorChoice(f.draft.Color)
		p.notice = ""
		return p
	case f.field == fieldPatterns+len(f.draft.Match):
		return p.addPattern()
	}
	return p.startInput(f.field)
}

// addPattern appends an empty pattern and opens its input.
func (p projectsPage) addPattern() projectsPage {
	p.form.draft.Match = append(p.form.draft.Match, "")
	return p.startInput(fieldPatterns + len(p.form.draft.Match) - 1)
}

// startInput opens the input on field i with its current text.
func (p projectsPage) startInput(i int) projectsPage {
	f := &p.form
	f.field = i
	f.editing = true
	f.input.SetValue(f.value(i))
	f.input.CursorEnd()
	f.input.Focus()
	p.notice, p.bad = "", false
	return p
}

// value is the text of field i.
func (f form) value(i int) string {
	switch i {
	case fieldName:
		return f.draft.Name
	case fieldDescription:
		return f.draft.Description
	case fieldColor:
		return f.draft.Color
	}
	if f.pattern(i) {
		return f.draft.Match[i-fieldPatterns]
	}
	return ""
}

// set puts text into field i.
func (f *form) set(i int, text string) {
	switch i {
	case fieldName:
		f.draft.Name = text
	case fieldDescription:
		f.draft.Description = text
	case fieldColor:
		f.draft.Color = text
	default:
		if f.pattern(i) {
			f.draft.Match[i-fieldPatterns] = text
		}
	}
	f.dirty = true
}

// inputKey handles a key while the input is open: enter takes the text,
// esc drops it (an empty new pattern is removed), the rest is typed.
func (p projectsPage) inputKey(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	f := &p.form
	switch {
	case key.Matches(msg, m.keys.Select):
		text := strings.TrimSpace(f.input.Value())
		if f.field == fieldColor {
			if _, err := project.ParseColor(text); err != nil {
				p.notice, p.bad = err.Error(), true
				return p, nil
			}
		}
		f.set(f.field, text)
		f.editing = false
		f.input.Blur()
		p.notice, p.bad = "", false
		return p, nil
	case key.Matches(msg, m.keys.Back):
		f.editing = false
		f.input.Blur()
		if f.pattern(f.field) && f.draft.Match[f.field-fieldPatterns] == "" {
			i := f.field - fieldPatterns
			f.draft.Match = append(f.draft.Match[:i:i], f.draft.Match[i+1:]...)
			f.field = min(f.field, f.nFields()-1)
		}
		p.notice, p.bad = "", false
		return p, nil
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return p, cmd
}

// Choices of the colour picker: automatic, the palette, a custom value.
func colorChoices() []string {
	out := []string{"automatic"}
	for _, c := range project.Palette {
		out = append(out, c.Name)
	}
	return append(out, "custom")
}

// colorChoice is the picker choice of a configured colour.
func colorChoice(color string) int {
	hex, err := project.ParseColor(color)
	if err != nil || hex == "" {
		return 0
	}
	for i, c := range project.Palette {
		if c.Hex == hex {
			return i + 1
		}
	}
	return len(project.Palette) + 1
}

// pickerKey moves the choice of the colour picker; enter takes it (custom
// opens the input for #rrggbb), esc closes the picker.
func (p projectsPage) pickerKey(m *Model, msg tea.KeyPressMsg) projectsPage {
	f := &p.form
	n := len(colorChoices())
	switch {
	case key.Matches(msg, m.keys.Prev), key.Matches(msg, m.keys.Up):
		f.pick = (f.pick + n - 1) % n
	case key.Matches(msg, m.keys.Next), key.Matches(msg, m.keys.Down):
		f.pick = (f.pick + 1) % n
	case key.Matches(msg, m.keys.Select):
		return p.pickColor(f.pick)
	case key.Matches(msg, m.keys.Back):
		f.picking = false
	}
	return p
}

// pickColor applies choice i of the picker.
func (p projectsPage) pickColor(i int) projectsPage {
	f := &p.form
	f.picking = false
	switch {
	case i == 0:
		f.set(fieldColor, "")
	case i <= len(project.Palette):
		f.set(fieldColor, project.Palette[i-1].Name)
	default:
		p = p.startInput(fieldColor)
		if colorChoice(f.draft.Color) != i {
			p.form.input.SetValue("#")
			p.form.input.CursorEnd()
		}
	}
	return p
}

// save writes the draft and returns to the list; a rejected draft leaves
// the file alone and shows why.
func (p projectsPage) save(m *Model) (page, tea.Cmd) {
	f := p.form
	var cfg *config.Config
	var err error
	if f.index < 0 {
		cfg, err = m.opts.Config.AddProject(f.draft)
	} else {
		cfg, err = m.opts.Config.SetProject(f.index, f.draft)
	}
	if err != nil {
		p.notice, p.bad = err.Error(), true
		return p, nil
	}
	p.mode = modeList
	p.want = f.draft.Name
	p.notice, p.bad = f.draft.Name+" written to "+m.tilde(cfg.Path)+applied, false
	return p, changed(cfg)
}

// ---- mouse ----

// click marks the row under the pointer; a click on the marked row acts
// like enter. In the form a click on a choice of the colour picker picks
// it.
func (p projectsPage) click(m *Model, msg tea.MouseClickMsg) (page, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.opts.Config == nil {
		return p, nil
	}
	switch p.mode {
	case modeDelete:
		return p, nil
	case modeForm:
		return p.formClick(m, msg), nil
	}
	i := p.offset + msg.Y - 1
	if msg.Y < 1 || i < 0 || i >= len(p.rows) || p.rows[i].kind == rowHeader || p.mode == modePick && p.rows[i].kind != rowDef {
		return p, nil
	}
	if i != p.row {
		p.row = i
		return p, nil
	}
	if p.mode == modePick {
		return p.pickKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	return p.open(m), nil
}

func (p projectsPage) formClick(m *Model, msg tea.MouseClickMsg) projectsPage {
	if p.form.editing {
		return p
	}
	if p.form.picking {
		if msg.Y == p.pickerRow(m) {
			_, spans := p.picker(m)
			if i := zone.At(spans, msg.X); i >= 0 {
				return p.pickColor(i)
			}
		}
		return p
	}
	i := msg.Y - 1
	if i < 0 || i >= p.form.nFields() {
		return p
	}
	if i != p.form.field {
		p.form.field = i
		return p
	}
	return p.activate()
}

// pickerRow is the row of the colour picker in the page of the last
// rendered height.
func (p projectsPage) pickerRow(m *Model) int { return m.height - chromeRows - 2 }

// ---- view ----

// cursor is the position of the text cursor while the input is open.
func (p projectsPage) cursor(height int) *tea.Cursor {
	if p.mode != modeForm || !p.form.editing {
		return nil
	}
	c := p.form.input.Cursor()
	if c == nil {
		return nil
	}
	c.X++ // the leading space
	c.Y = height - 1
	return c
}

// listBottom are the rows below the list: rule, detail, status.
const listBottom = 3

func (p projectsPage) view(m *Model, width, height int) string {
	cfg := m.opts.Config
	if cfg == nil {
		return " " + m.th.Muted.Render("no configuration")
	}
	if p.mode == modeForm {
		return p.formView(m, width, height)
	}
	avail := max(height-1-listBottom, 1)
	if p.row < p.offset {
		p.offset = p.row
	}
	if p.row >= p.offset+avail {
		p.offset = p.row - avail + 1
	}
	p.offset = max(0, min(p.offset, max(len(p.rows)-avail, 0)))
	r, rerr := pageResolver(cfg)
	as := assign(r, p.ar)
	nameW, descW := 4, 11
	for _, d := range cfg.Projects {
		nameW = max(nameW, lipgloss.Width(d.Name))
		descW = max(descW, lipgloss.Width(d.Description))
	}
	for _, i := range as.unmatched {
		nameW = max(nameW, lipgloss.Width(p.ar.name(i)))
	}
	nameW, descW = min(nameW, 24), min(descW, 40)
	const sessW, lastW = 8, 11
	lines := make([]string, 0, height)
	header := "  " + pad("NAME", nameW) + "  " + pad("DESCRIPTION", descW) + "  " + pad("SESSIONS", sessW) + "  MATCH"
	if len(cfg.Projects) == 0 {
		header = "  no project definitions yet: sessions are named after their git root or directory"
	}
	lines = append(lines, m.line(" "+m.th.Muted.Render(header), width))
	end := min(p.offset+avail, len(p.rows))
	for i := p.offset; i < end; i++ {
		row := p.rows[i]
		mark := " "
		if i == p.row {
			mark = m.th.Mark.Render("▸")
		}
		var line string
		switch row.kind {
		case rowHeader:
			if row.label == "" {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, m.line("   "+m.th.Muted.Render(pad(row.label, nameW)+"  "+pad("SESSIONS", sessW)+"  "+pad("LAST", lastW)+"  PATH"), width))
			continue
		case rowDef:
			d := cfg.Projects[row.index]
			paths, sessions := p.ar.tally(as.byName[d.Name])
			count := m.th.Muted.Render(pad("", sessW))
			if paths > 0 {
				count = fmt.Sprintf("%*d", sessW, sessions)
			}
			line = p.swatch(m, d.Color) + " " + pad(m.line(d.Name, nameW), nameW) + "  " + pad(m.line(d.Description, descW), descW) + "  " + count + "  " + strings.Join(d.Match, ", ")
		case rowKey:
			k := p.ar.keys[row.index]
			path := m.tilde(k.Key)
			if k.Remote != "" {
				path += "  " + m.th.Muted.Render(k.Remote)
			}
			line = pad(m.line(p.ar.name(row.index), nameW), nameW) + "  " + fmt.Sprintf("%*d", sessW, k.Sessions) + "  " + pad(last(m, k.Last), lastW) + "  " + path
		}
		if i == p.row {
			line = m.th.Selected.Render(pad(m.line(line, width-2), width-2))
		}
		lines = append(lines, m.line(mark+" "+line, width))
	}
	for len(lines) < height-listBottom {
		lines = append(lines, "")
	}
	lines = append(lines, m.th.Rule.Render(strings.Repeat("─", width)))
	lines = append(lines, m.line(" "+p.detail(m, r, as), width))
	switch {
	case p.mode == modeDelete:
		d := cfg.Projects[p.current().index]
		lines = append(lines, m.line(" "+m.th.Error.Render(fmt.Sprintf("remove %s from %s? %s confirms, %s cancels", d.Name, m.tilde(cfg.Path), m.keys.Confirm.Help().Key, m.keys.Back.Help().Key)), width))
	case p.mode == modePick:
		lines = append(lines, m.line(" "+m.th.Label.Render(fmt.Sprintf("add %q to the marked project? %s confirms, %s cancels", p.pending, m.keys.Select.Help().Key, m.keys.Back.Help().Key)), width))
	case rerr != nil:
		lines = append(lines, m.line(" "+m.th.Error.Render("[[project]] has a problem: "+rerr.Error()), width))
	case p.notice != "" && p.bad:
		lines = append(lines, m.line(" "+m.th.Error.Render(p.notice), width))
	case p.notice != "":
		lines = append(lines, m.line(" "+m.th.Muted.Render(p.notice), width))
	case p.ar.err != nil:
		lines = append(lines, m.line(" "+m.th.Error.Render("archive: "+p.ar.err.Error()), width))
	default:
		k := m.keys
		lines = append(lines, m.line(" "+m.th.Muted.Render(fmt.Sprintf("%s edits · %s adds · %s removes · %s/%s move (first match wins) · %s adds a path as a pattern", k.Select.Help().Key, k.Add.Help().Key, k.Remove.Help().Key, k.MoveUp.Help().Key, k.MoveDn.Help().Key, k.Pattern.Help().Key)), width))
	}
	return strings.Join(lines, "\n")
}

// last formats the time of the newest session of a path.
func last(m *Model, t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return age(m.now(), t)
}

// swatch is the colour field of a definition: a block in its colour, a
// dot for the automatic colour.
func (p projectsPage) swatch(m *Model, color string) string {
	hex, err := project.ParseColor(color)
	if err != nil || hex == "" {
		return m.th.Muted.Render("·")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("■")
}

// detail describes the marked row: a definition with its colour, what it
// gets of the archive and what earlier definitions take from it; a path
// with its sessions and the keys that make something of it.
func (p projectsPage) detail(m *Model, r project.Resolver, as assignment) string {
	cfg := m.opts.Config
	row := p.current()
	switch row.kind {
	case rowDef:
		d := cfg.Projects[row.index]
		colour := "automatic colour"
		if hex, err := project.ParseColor(d.Color); err == nil && hex != "" {
			colour = hex
		} else if d.Color != "" {
			colour = d.Color + "?"
		}
		what := ""
		if p.ar.loaded {
			hs, err := hits(r, d.Name, d.Match, p.ar, cfg.Home)
			if err != nil {
				what = err.Error()
			} else {
				what = p.ar.describe(hs)
			}
		}
		return m.th.Label.Render(d.Name) + "  " + m.th.Muted.Render(colour) + "  " + what
	case rowKey:
		k := p.ar.keys[row.index]
		k2 := m.keys
		return m.th.Label.Render(p.ar.name(row.index)) + "  " + m.th.Muted.Render(fmt.Sprintf("%s, last %s · %s makes it a project, %s adds it to one", plural(k.Sessions, "session"), last(m, k.Last), k2.Add.Help().Key, k2.Pattern.Help().Key))
	}
	if len(cfg.Projects) == 0 && len(as.unmatched) == 0 && p.ar.loaded {
		return m.th.Muted.Render("[[project]] gives a project its name, colour and patterns; " + m.keys.Add.Help().Key + " adds one")
	}
	return ""
}

// formBottom are the rows below the fields: rule, preview, picker or
// help, input or status.
const formBottom = 4

func (p projectsPage) formView(m *Model, width, height int) string {
	cfg := m.opts.Config
	f := p.form
	lines := make([]string, 0, height)
	title := " " + m.th.Title.Render("new project")
	if f.index >= 0 {
		title = " " + m.th.Title.Render("project "+cfg.Projects[f.index].Name) + "  " + m.th.Muted.Render(fmt.Sprintf("%d of %d · first match wins", f.index+1, len(cfg.Projects)))
	}
	lines = append(lines, m.line(title, width))
	const labelW = 12
	for i := 0; i < f.nFields(); i++ {
		label, value := "", ""
		switch {
		case i == fieldName:
			label, value = "name", f.draft.Name
			if value == "" {
				value = m.th.Muted.Render("required")
			}
		case i == fieldDescription:
			label, value = "description", f.draft.Description
			if value == "" {
				value = m.th.Muted.Render("none")
			}
		case i == fieldColor:
			label = "color"
			if hex, err := project.ParseColor(f.draft.Color); err == nil && hex != "" {
				value = p.swatch(m, f.draft.Color) + " " + f.draft.Color
				if f.draft.Color != hex {
					value += " " + m.th.Muted.Render(hex)
				}
			} else {
				value = p.swatch(m, "") + " " + m.th.Muted.Render("automatic")
			}
		case f.pattern(i):
			label, value = "match", f.draft.Match[i-fieldPatterns]
			if value == "" {
				value = m.th.Muted.Render("empty")
			}
		default:
			value = m.th.Muted.Render("+ pattern")
		}
		mark := " "
		if i == f.field {
			mark = m.th.Mark.Render("▸")
		}
		line := pad(label, labelW) + "  " + value
		if i == f.field && !f.editing {
			line = m.th.Selected.Render(pad(m.line(line, width-2), width-2))
		}
		lines = append(lines, m.line(mark+" "+line, width))
	}
	for len(lines) < height-formBottom {
		lines = append(lines, "")
	}
	lines = append(lines, m.th.Rule.Render(strings.Repeat("─", width)))
	lines = append(lines, m.line(" "+p.preview(m), width))
	k := m.keys
	switch {
	case f.picking:
		row, _ := p.picker(m)
		lines = append(lines, m.line(row, width))
	case f.editing && f.pattern(f.field):
		lines = append(lines, m.line(" "+m.th.Muted.Render("* matches any text (also across /), ? one character · against the path, the path with ~, the name and the remote URL"), width))
	case f.editing && f.field == fieldColor:
		lines = append(lines, m.line(" "+m.th.Muted.Render("a palette name or #rrggbb; empty is the automatic colour"), width))
	default:
		lines = append(lines, m.line(" "+m.th.Muted.Render(fmt.Sprintf("%s edits the marked field · %s adds a pattern · %s removes it · %s saves · %s leaves", k.Select.Help().Key, k.Add.Help().Key, k.Remove.Help().Key, k.Confirm.Help().Key, k.Back.Help().Key)), width))
	}
	switch {
	case f.editing && p.notice != "":
		lines = append(lines, m.line(" "+f.input.View()+"  "+m.th.Error.Render(p.notice), width))
	case f.editing:
		lines = append(lines, m.line(" "+f.input.View()+"  "+m.th.Muted.Render(k.Select.Help().Key+" takes it, "+k.Back.Help().Key+" cancels"), width))
	case p.notice != "" && p.bad:
		lines = append(lines, m.line(" "+m.th.Error.Render(p.notice), width))
	case p.notice != "":
		lines = append(lines, m.line(" "+m.th.Muted.Render(p.notice), width))
	case f.dirty:
		lines = append(lines, m.line(" "+m.th.Muted.Render("unsaved changes"), width))
	default:
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// preview says what the draft gets of the archive: the marked pattern
// (with the text of the open input) alone, else all patterns together,
// and which earlier definition takes a path.
func (p projectsPage) preview(m *Model) string {
	cfg := m.opts.Config
	f := p.form
	if !p.ar.loaded {
		return m.th.Muted.Render("preview: the archive is still loading")
	}
	draft := f.draft
	draft.Match = append(config.StringList{}, draft.Match...)
	if f.editing && f.pattern(f.field) {
		draft.Match[f.field-fieldPatterns] = strings.TrimSpace(f.input.Value())
	}
	if draft.Name == "" {
		return m.th.Muted.Render("preview: a name is required")
	}
	var patterns []string
	for _, pt := range draft.Match {
		if pt != "" {
			patterns = append(patterns, pt)
		}
	}
	if len(patterns) == 0 {
		return m.th.Muted.Render("preview: a pattern is required, e.g. the remote URL or ~/src/name")
	}
	draft.Match = patterns
	r, err := draftResolver(cfg, f.index, draft)
	if err != nil {
		return m.th.Error.Render("preview: " + err.Error())
	}
	label, subject := "preview", patterns
	if f.pattern(f.field) {
		pt := strings.TrimSpace(f.value(f.field))
		if f.editing {
			pt = strings.TrimSpace(f.input.Value())
		}
		if pt != "" {
			label, subject = "pattern "+pt, []string{pt}
		}
	}
	hs, err := hits(r, draft.Name, subject, p.ar, cfg.Home)
	if err != nil {
		return m.th.Error.Render("preview: " + err.Error())
	}
	return m.th.Label.Render(label) + "  " + p.ar.describe(hs)
}

// picker renders the colour choices in one row, the marked one in
// brackets, each with a swatch, and the span of each choice for clicks.
func (p projectsPage) picker(m *Model) (string, []zone.Span) {
	var b strings.Builder
	b.WriteString(" color: ")
	spans := make([]zone.Span, 0, len(project.Palette)+2)
	for i, name := range colorChoices() {
		start := lipgloss.Width(b.String())
		swatch := ""
		switch {
		case i == 0:
			swatch = m.th.Muted.Render("·")
		case i <= len(project.Palette):
			swatch = lipgloss.NewStyle().Foreground(lipgloss.Color(project.Palette[i-1].Hex)).Render("■")
		default:
			name = "custom #rrggbb"
			swatch = " "
		}
		text := name + " "
		if i == p.form.pick {
			text = m.th.Active.Render("[" + name + "]")
		}
		b.WriteString(swatch + " " + text + " ")
		spans = append(spans, zone.Span{Start: start, End: lipgloss.Width(b.String())})
	}
	return b.String(), spans
}
