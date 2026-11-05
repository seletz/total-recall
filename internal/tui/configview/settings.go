package configview

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/config"
)

// settingsRow is one line of the settings list: a table header or a key
// with its value.
type settingsRow struct {
	key    string // dotted key; empty for a header
	label  string // what the key column shows
	header bool
	value  string
	origin config.Origin
	desc   config.Key
}

// editedMsg is the return of the external editor.
type editedMsg struct{ err error }

// settingsPage lists every key of the configuration and edits one at a
// time: bool and enum values switch to the next value on select, the
// others open a text input in the bottom line.
type settingsPage struct {
	rows    []settingsRow
	row     int
	offset  int
	editing bool
	input   textinput.Model
	// notice is the last outcome, shown in the bottom line; bad marks an
	// error.
	notice string
	bad    bool
}

func newSettingsPage(m *Model) settingsPage {
	in := textinput.New()
	in.Prompt = "> "
	in.SetVirtualCursor(false)
	p := settingsPage{input: in}
	p.rows = buildRows(m)
	p.row = p.next(-1, 1)
	return p
}

// buildRows lists the entries of the configuration grouped by table, in
// the order of `config show`.
func buildRows(m *Model) []settingsRow {
	cfg := m.opts.Config
	if cfg == nil {
		return nil
	}
	var rows []settingsRow
	table := ""
	for _, e := range cfg.Entries() {
		desc, _ := config.Lookup(e.Key)
		t, label := "", e.Key
		if i := strings.LastIndex(e.Key, "."); i >= 0 {
			t, label = e.Key[:i], e.Key[i+1:]
		}
		if desc.Kind == config.KindTable {
			// [accounts] and [[worktime.category]] are shown as a whole
			// under their own header.
			t, label = e.Key, e.Key
			if i := strings.LastIndex(e.Key, "."); i >= 0 {
				label = e.Key[i+1:]
			}
		}
		if t != table {
			table = t
			if t != "" {
				rows = append(rows, settingsRow{label: "[" + t + "]", header: true})
			}
		}
		rows = append(rows, settingsRow{key: e.Key, label: label, value: display(desc, e.Value, m), origin: e.Origin, desc: desc})
	}
	return rows
}

// display formats a value for the list: durations the short way, paths
// with "~".
func display(k config.Key, v string, m *Model) string {
	switch k.Kind {
	case config.KindDuration:
		if d, err := time.ParseDuration(v); err == nil {
			return config.FormatDuration(d)
		}
	case config.KindPath:
		return m.tilde(v)
	}
	return v
}

func (settingsPage) title() string { return "settings" }

func (p settingsPage) focused() bool { return p.editing }

func (p settingsPage) shortHelp(m *Model) []key.Binding {
	if p.editing {
		return []key.Binding{m.keys.Select, m.keys.Back}
	}
	return []key.Binding{m.keys.Move, m.keys.Select, m.keys.Default, m.keys.Editor}
}

func (p settingsPage) fullHelp(m *Model) [][]key.Binding {
	return [][]key.Binding{{m.keys.Up, m.keys.Down, m.keys.Select}, {m.keys.Default, m.keys.Editor, m.keys.Back}}
}

// reload rebuilds the rows from the current configuration, keeping the
// cursor on the same key.
func (p settingsPage) reload(m *Model) (page, tea.Cmd) {
	key := p.current().key
	p.rows = buildRows(m)
	p.row = p.next(-1, 1)
	for i, r := range p.rows {
		if r.key == key && key != "" {
			p.row = i
		}
	}
	return p, nil
}

func (p settingsPage) update(m *Model, msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case Changed:
		return p.reload(m)
	case editedMsg:
		var cmd tea.Cmd
		p.notice, p.bad, cmd = m.reloadFile(msg)
		return p, cmd
	case tea.KeyPressMsg:
		if p.editing {
			return p.editKey(m, msg)
		}
		return p.key(m, msg)
	case tea.MouseClickMsg:
		return p.click(m, msg)
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			return p.move(-1), nil
		}
		return p.move(1), nil
	}
	return p, nil
}

func (p settingsPage) key(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		return p.move(-1), nil
	case key.Matches(msg, m.keys.Down):
		return p.move(1), nil
	case key.Matches(msg, m.keys.Select):
		return p.selectRow(m)
	case key.Matches(msg, m.keys.Default):
		return p.reset(m)
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

// editKey handles a key while the input is open: enter writes, esc
// cancels, everything else is text.
func (p settingsPage) editKey(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Select):
		return p.commit(m, p.input.Value())
	case key.Matches(msg, m.keys.Back):
		p.editing = false
		p.input.Blur()
		p.notice, p.bad = "", false
		return p, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

// click marks the row under the pointer; a click on the marked row
// selects it.
func (p settingsPage) click(m *Model, msg tea.MouseClickMsg) (page, tea.Cmd) {
	if msg.Button != tea.MouseLeft || p.editing {
		return p, nil
	}
	i := p.offset + msg.Y - 1 // the column header
	if msg.Y < 1 || i < 0 || i >= len(p.rows) || p.rows[i].header {
		return p, nil
	}
	if i == p.row {
		return p.selectRow(m)
	}
	p.row = i
	return p, nil
}

// current is the marked row (a zero row without rows).
func (p settingsPage) current() settingsRow {
	if p.row < 0 || p.row >= len(p.rows) {
		return settingsRow{}
	}
	return p.rows[p.row]
}

// next returns the index of the first non-header row after from in
// direction dir, or from when there is none.
func (p settingsPage) next(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(p.rows); i += dir {
		if !p.rows[i].header {
			return i
		}
	}
	return from
}

func (p settingsPage) move(dir int) settingsPage {
	if n := p.next(p.row, dir); n != p.row && n >= 0 {
		p.row = n
	}
	p.notice = ""
	return p
}

// selectRow acts on the marked key: a bool toggles, an enum takes the
// next value, a table is edited in the file, anything else opens the
// input with the current value.
func (p settingsPage) selectRow(m *Model) (page, tea.Cmd) {
	r := p.current()
	if r.key == "" {
		return p, nil
	}
	switch {
	case r.origin == config.OriginEnv || r.origin == config.OriginFlag:
		p.notice, p.bad = fmt.Sprintf("%s is set by %s; a value in the file would not apply", r.key, r.origin), true
		return p, nil
	case r.desc.Kind == config.KindTable:
		p.notice, p.bad = r.key+" is a table: edit it in the file (E opens $EDITOR)", true
		return p, nil
	case r.desc.Kind == config.KindBool:
		if r.value == "true" {
			return p.commit(m, "false")
		}
		return p.commit(m, "true")
	case r.desc.Kind == config.KindEnum:
		next := r.desc.Enum[0]
		for i, v := range r.desc.Enum {
			if v == r.value && i+1 < len(r.desc.Enum) {
				next = r.desc.Enum[i+1]
			}
		}
		return p.commit(m, next)
	}
	p.editing = true
	p.input.SetValue(r.value)
	p.input.CursorEnd()
	p.input.Focus()
	p.notice, p.bad = "", false
	return p, nil
}

// commit writes text as the value of the marked key and reports the
// outcome; the new configuration goes to the app as Changed.
func (p settingsPage) commit(m *Model, text string) (page, tea.Cmd) {
	r := p.current()
	cfg, err := m.opts.Config.SetValue(r.key, text)
	if err != nil {
		p.notice, p.bad = err.Error(), true
		return p, nil
	}
	p.editing = false
	p.input.Blur()
	p.notice, p.bad = r.key+" written to "+m.tilde(cfg.Path)+" · "+effect(r.key), false
	return p, func() tea.Msg { return Changed{Config: cfg} }
}

// What a change of a key does, as the page words it in the notice after a
// write and on the default line of the marked key.
const (
	// effectNow: the running TUI takes the value at once.
	effectNow = "applied at once"
	// effectStart: the TUI reads the value at its start only.
	effectStart = "takes effect at the next start"
	// effectCommand: the TUI never reads the value; the hooks, the
	// commands and the actions of this view load the file when they run.
	effectCommand = "used by the next command or action"
)

// effect says what a change of key does. The app applies the reload
// interval, the mouse, the key bindings, the project definitions, the day
// boundary, the working time and the start modes of the activity view at
// once (see tui.App); the archive, the time zone, the view shown first,
// the chart mode and the start states of the stats view and the activity
// window are read at the start only. Everything else is for the commands.
func effect(key string) string {
	switch {
	case key == "day_start", key == "project", key == "tui.mouse", key == "tui.reload", key == "tui.activity_projects",
		strings.HasPrefix(key, "work."), strings.HasPrefix(key, "keys."):
		return effectNow
	case key == "db", key == "timezone", strings.HasPrefix(key, "tui."):
		return effectStart
	}
	return effectCommand
}

// reset removes the marked key from the file.
func (p settingsPage) reset(m *Model) (page, tea.Cmd) {
	r := p.current()
	if r.key == "" {
		return p, nil
	}
	if r.desc.Kind == config.KindTable {
		p.notice, p.bad = r.key+" is a table: edit it in the file (E opens $EDITOR)", true
		return p, nil
	}
	cfg, err := m.opts.Config.Reset(r.key)
	if err != nil {
		p.notice, p.bad = err.Error(), true
		return p, nil
	}
	if cfg == m.opts.Config {
		p.notice, p.bad = r.key+" is not set in the file", false
		return p, nil
	}
	p.notice, p.bad = r.key+" reset to the default · "+effect(r.key), false
	return p, func() tea.Msg { return Changed{Config: cfg} }
}

// editor opens the config file in $EDITOR (written from the template
// first when it does not exist); the editedMsg of the returned command
// reloads it. Without an editor it returns the reason and no command.
func (m *Model) editor() (string, tea.Cmd) {
	ed := m.opts.Getenv("EDITOR")
	if ed == "" {
		return "set $EDITOR to edit the file from here", nil
	}
	path := m.opts.Config.Path
	if err := config.WriteTemplate(path); err != nil && !errors.Is(err, fs.ErrExist) {
		return err.Error(), nil
	}
	args := strings.Fields(ed)
	c := exec.Command(args[0], append(args[1:], path)...) //nolint:gosec // the user's own $EDITOR
	return "", tea.ExecProcess(c, func(err error) tea.Msg { return editedMsg{err: err} })
}

// reloadFile loads the configuration file again after the editor closed
// and hands the result on as Changed.
func (m *Model) reloadFile(msg editedMsg) (string, bool, tea.Cmd) {
	if msg.err != nil {
		return "editor: " + msg.err.Error(), true, nil
	}
	cfg, err := m.opts.Config.Reload()
	if err != nil {
		return err.Error(), true, nil
	}
	return "reloaded " + m.tilde(cfg.Path), false, func() tea.Msg { return Changed{Config: cfg} }
}

// cursor is the position of the text cursor in the page while editing.
func (p settingsPage) cursor(height int) *tea.Cursor {
	if !p.editing {
		return nil
	}
	c := p.input.Cursor()
	if c == nil {
		return nil
	}
	c.X++ // the leading space
	c.Y = height - 1
	return c
}

// bottomRows are the rows below the list: rule, help, default, status.
const bottomRows = 4

// view renders the column header, the rows that fit, and the bottom block
// with the help of the marked key, its default and the notice or the
// input.
func (p settingsPage) view(m *Model, width, height int) string {
	if m.opts.Config == nil {
		return " " + m.th.Muted.Render("no configuration")
	}
	avail := max(height-1-bottomRows, 1)
	// Keep the cursor visible.
	if p.row < p.offset {
		p.offset = p.row
	}
	if p.row >= p.offset+avail {
		p.offset = p.row - avail + 1
	}
	p.offset = max(0, min(p.offset, max(len(p.rows)-avail, 0)))
	keyW := 0
	for _, r := range p.rows {
		if !r.header {
			keyW = max(keyW, lipgloss.Width(r.label))
		}
	}
	keyW = min(max(keyW, 3), 36)
	const originW = 7
	valueW := max(width-2-keyW-2-originW-2, 4)
	lines := make([]string, 0, height)
	lines = append(lines, m.line(" "+m.th.Muted.Render(pad("KEY", keyW+1)+" "+pad("VALUE", valueW)+"  ORIGIN"), width))
	end := min(p.offset+avail, len(p.rows))
	for i := p.offset; i < end; i++ {
		r := p.rows[i]
		if r.header {
			lines = append(lines, m.line("  "+m.th.Title.Render(r.label), width))
			continue
		}
		mark := " "
		if i == p.row {
			mark = m.th.Mark.Render("▸")
		}
		value := r.value
		if r.value == "" {
			value = m.th.Muted.Render(`""`)
		}
		origin := string(r.origin)
		if r.origin != config.OriginDefault {
			origin = m.th.AccentText.Render(origin)
		} else {
			origin = m.th.Muted.Render(origin)
		}
		line := pad(r.label, keyW) + "  " + pad(m.line(value, valueW), valueW) + "  " + origin
		if i == p.row {
			line = m.th.Selected.Render(line)
		}
		lines = append(lines, m.line(mark+" "+line, width))
	}
	for len(lines) < height-bottomRows {
		lines = append(lines, "")
	}
	lines = append(lines, m.th.Rule.Render(strings.Repeat("─", width)))
	r := p.current()
	help := ""
	if r.key != "" {
		help = m.th.Label.Render(r.key) + "  " + r.desc.Help
		if r.desc.Kind == config.KindEnum {
			help += " (" + strings.Join(r.desc.Enum, ", ") + ")"
		}
	}
	lines = append(lines, m.line(" "+help, width))
	def := ""
	if r.key != "" {
		def = m.th.Muted.Render("default: " + display(r.desc, m.opts.Config.Default(r.key), m) + " · change: " + effect(r.key))
	}
	lines = append(lines, m.line(" "+def, width))
	switch {
	case p.editing && p.notice != "":
		lines = append(lines, m.line(" "+p.input.View()+"  "+m.th.Error.Render(p.notice), width))
	case p.editing:
		lines = append(lines, m.line(" "+p.input.View()+"  "+m.th.Muted.Render("enter saves, esc cancels"), width))
	case p.notice != "" && p.bad:
		lines = append(lines, m.line(" "+m.th.Error.Render(p.notice), width))
	case p.notice != "":
		lines = append(lines, m.line(" "+m.th.Muted.Render(p.notice), width))
	default:
		lines = append(lines, m.line(" "+m.th.Muted.Render("enter edits the marked key, d resets it to the default"), width))
	}
	return strings.Join(lines, "\n")
}
