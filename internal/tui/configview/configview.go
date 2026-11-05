// Package configview is the config tab of the TUI: five pages behind one
// switch row. Status shows what `status` shows (archive, size, schema and a
// pending migration, counts, last ingest and backup, errors of the log,
// problems of the config). Settings lists every key of the configuration
// with value, origin and default and edits it in place: the value is
// parsed by its kind, the file is written with its comments kept (see
// config.SetValue), and the result is loaded like Load before anything is
// written. Actions runs backup, maintenance, migrate, reindex, ingest and
// doctor in process with the functions the CLI uses, after a confirmation
// for everything that writes, with progress lines and a cancel. Setup
// checks the installation (config file, hooks in the harness settings,
// binary on PATH, backup directory, transcripts, archive) and shows the
// hook block to copy. Projects is a placeholder for the project
// definitions (#4).
//
// The view holds the state the pages share (the loaded status) and
// forwards keys to the active page; a page that edits text takes the
// keyboard (Focused). A change of the configuration reaches the app as
// Changed; while an action runs, Busy pauses the automatic reload.
package configview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// Options configure the view.
type Options struct {
	// Config is the effective configuration; nil means none (the pages
	// show a hint).
	Config *config.Config
	// Store is the read-only archive; nil means there is none.
	Store *store.Store
	// Location is the time zone of the times shown; nil means UTC.
	Location *time.Location
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
	// Home is the home directory, shown as "~" in paths; empty keeps
	// full paths.
	Home string
	// Version is the version of the running binary, compared with the
	// one on PATH.
	Version string
	// Getenv looks up environment variables (CLAUDE_CONFIG_DIR, EDITOR);
	// nil means os.Getenv.
	Getenv func(string) string
	// SettingsFile is the settings file of Claude Code whose hooks the
	// setup page checks; empty means $CLAUDE_CONFIG_DIR/settings.json or
	// ~/.claude/settings.json.
	SettingsFile string
	// LookPath finds the binary on PATH; nil means exec.LookPath.
	LookPath func(file string) (string, error)
	// Executable is the path of the running binary; nil means
	// os.Executable.
	Executable func() (string, error)
	// VersionOf asks the binary at path for its version; nil runs
	// "<path> --version".
	VersionOf func(path string) (string, error)
	// Wait is how long maintenance waits for running hooks; zero means a
	// minute.
	Wait time.Duration
}

func (o *Options) defaults() {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Executable == nil {
		o.Executable = os.Executable
	}
	if o.VersionOf == nil {
		o.VersionOf = versionOf
	}
	if o.Wait == 0 {
		o.Wait = time.Minute
	}
	if o.SettingsFile == "" {
		dir := o.Getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(o.Home, ".claude")
		}
		o.SettingsFile = filepath.Join(dir, "settings.json")
	}
}

// versionOf runs "<path> --version" and returns the first line.
func versionOf(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	v, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return v, nil
}

// Changed says the configuration file was written: Config is the new
// effective configuration. The app applies what concerns it (reload
// interval, mouse); everything else takes effect at the next start.
type Changed struct {
	Config *config.Config
}

// Busy says an action that writes to the archive started (On) or ended;
// the app pauses its automatic reload in between and reloads after.
type Busy struct {
	On bool
}

// page is one page of the view. Pages are values: an update returns the
// page to keep.
type page interface {
	// title names the page in the switch row.
	title() string
	// update handles a message meant for the page (keys and clicks only
	// while it is active, results always).
	update(m *Model, msg tea.Msg) (page, tea.Cmd)
	// view renders the page into width×height cells.
	view(m *Model, width, height int) string
	// reload loads the page's data again.
	reload(m *Model) (page, tea.Cmd)
	// focused reports whether the page takes every key (text input).
	focused() bool
	// cursor is where the text cursor stands in a page of the given
	// height, nil without one.
	cursor(height int) *tea.Cursor
	// shortHelp and fullHelp are the page's bindings for the footer and
	// the help overlay.
	shortHelp(m *Model) []key.Binding
	fullHelp(m *Model) [][]key.Binding
}

// Page indices, the order of the switch row.
const (
	pageStatus = iota
	pageSettings
	pageProjects
	pageActions
	pageSetup
)

// Model is the config view.
type Model struct {
	opts   Options
	keys   KeyMap
	pages  []page
	active int
	// state is the loaded status, shared by the pages (the actions page
	// needs the schema versions).
	state   state
	loading bool
	seq     uint64
	// busy is set while an action runs: no reload meanwhile.
	busy          bool
	width, height int
	th            theme.Theme
}

// New builds the view.
func New(opts Options) Model {
	opts.defaults()
	m := Model{opts: opts, keys: newKeyMap(opts.Keys), th: theme.New(true), loading: true}
	m.pages = []page{newStatusPage(), newSettingsPage(&m), projectsPage{}, newActionsPage(), newSetupPage()}
	return m
}

// Title names the view in the header.
func (Model) Title() string { return "config" }

// Init loads the status and the setup checks.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.stateCmd()}
	for i := range m.pages {
		var cmd tea.Cmd
		m.pages[i], cmd = m.pages[i].reload(&m)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// Reload loads the status and the active page again.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	cmd := m.loadState()
	p, pcmd := m.pages[m.active].reload(&m)
	m.pages = m.replace(m.active, p)
	return m, tea.Batch(cmd, pcmd)
}

// Focused reports whether the active page takes every key.
func (m Model) Focused() bool { return m.pages[m.active].focused() }

// ShortHelp returns the footer bindings: the page switch and the active
// page's keys.
func (m Model) ShortHelp() []key.Binding {
	return append([]key.Binding{m.keys.Pages}, m.pages[m.active].shortHelp(&m)...)
}

// FullHelp returns every binding for the help overlay.
func (m Model) FullHelp() [][]key.Binding {
	return append([][]key.Binding{{m.keys.Next, m.keys.Prev}}, m.pages[m.active].fullHelp(&m)...)
}

// replace returns a copy of the pages with pages[i] set to p.
func (m Model) replace(i int, p page) []page {
	out := make([]page, len(m.pages))
	copy(out, m.pages)
	out[i] = p
	return out
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.broadcast(msg)
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
		return m.broadcast(msg)
	case stateMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.loading = false
		m.state = msg.state
		return m.broadcast(msg)
	case Changed:
		m.opts.Config = msg.Config
		cmd := m.loadState()
		_, bcmd := m.broadcast(msg)
		return m, tea.Batch(cmd, bcmd)
	case Busy:
		m.busy = msg.On
		if !msg.On {
			// The archive changed: load the status again.
			return m, m.loadState()
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseClickMsg:
		return m.click(msg)
	case tea.MouseWheelMsg:
		return m.wheel(msg)
	}
	return m.broadcast(msg)
}

// broadcast passes msg to every page.
func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, len(m.pages))
	pages := make([]page, len(m.pages))
	for i, p := range m.pages {
		pages[i], cmds[i] = p.update(&m, msg)
	}
	m.pages = pages
	return m, tea.Batch(cmds...)
}

// forward passes msg to the active page only.
func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	p, cmd := m.pages[m.active].update(&m, msg)
	m.pages = m.replace(m.active, p)
	return m, cmd
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pages[m.active].focused() {
		return m.forward(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Next):
		return m.show((m.active + 1) % len(m.pages))
	case key.Matches(msg, m.keys.Prev):
		return m.show((m.active + len(m.pages) - 1) % len(m.pages))
	}
	return m.forward(msg)
}

// show switches to page i and reloads it.
func (m Model) show(i int) (tea.Model, tea.Cmd) {
	m.active = i
	p, cmd := m.pages[i].reload(&m)
	m.pages = m.replace(i, p)
	return m, cmd
}

// click switches the page on the switch row, else goes to the active
// page with the row of the body.
func (m Model) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if msg.Y == 0 {
		_, spans := m.switches()
		if i := zone.At(spans, msg.X); i >= 0 {
			return m.show(i)
		}
		return m, nil
	}
	if msg.Y < chromeRows {
		return m, nil
	}
	msg.Y -= chromeRows
	return m.forward(msg)
}

func (m Model) wheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if msg.Y < chromeRows {
		return m, nil
	}
	msg.Y -= chromeRows
	return m.forward(msg)
}

// chromeRows are the rows above the page: the switch row and a rule.
const chromeRows = 2

// View renders the switch row, a rule and the active page.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	switches, _ := m.switches()
	body := m.pages[m.active].view(&m, m.width, max(m.height-chromeRows, 0))
	h := max(m.height-chromeRows, 0)
	st := lipgloss.NewStyle().Width(m.width).Height(h).MaxWidth(m.width).MaxHeight(h)
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, switches, m.th.Rule.Render(strings.Repeat("─", m.width)), st.Render(body)))
	if c := m.pages[m.active].cursor(h); c != nil {
		c.Y += chromeRows
		v.Cursor = c
	}
	return v
}

// switches is the top row: the page names with the active one in
// brackets, the config file on the right.
func (m Model) switches() (string, []zone.Span) {
	names := make([]string, len(m.pages))
	for i, p := range m.pages {
		names[i] = p.title()
	}
	left := " "
	toggles, spans := zone.Toggles(m.th.Active, names, m.active, lipgloss.Width(left))
	left += toggles
	right := ""
	if m.opts.Config != nil {
		right = m.th.Muted.Render(m.tilde(m.opts.Config.Path)) + " "
	}
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	line := left + strings.Repeat(" ", gap) + right
	if lipgloss.Width(line) > m.width {
		line = ansi.Truncate(left, m.width, "…")
		spans = zone.Clip(spans, max(m.width-1, 0))
	}
	return line, spans
}

// tilde abbreviates the home directory in a path.
func (m Model) tilde(p string) string {
	if m.opts.Home == "" || p == "" {
		return p
	}
	if p == m.opts.Home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(p, m.opts.Home+string(filepath.Separator)); ok {
		return "~/" + rel
	}
	return p
}

// now is the clock in the view's time zone.
func (m Model) now() time.Time { return m.opts.Now().In(m.opts.Location) }

// line cuts s to the width of the view.
func (m Model) line(s string, width int) string { return ansi.Truncate(s, width, "…") }

// mib formats bytes as MiB with one decimal.
func mib(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20)) }

// age formats how long ago t was: "45s ago", "12m ago", "3h05m ago",
// "2d04h ago".
func age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "in the future"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm ago", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh ago", int(d.Hours())/24, int(d.Hours())%24)
}

// pad fills s with spaces to n cells.
func pad(s string, n int) string { return s + strings.Repeat(" ", max(n-lipgloss.Width(s), 0)) }

// plural writes "1 session", "2 sessions".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
