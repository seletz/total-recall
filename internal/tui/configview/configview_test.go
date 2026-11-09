package configview

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// bodySize is the window size the app shell hands the view at 100×30.
var bodySize = tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height - 4}

// testEnv is a config view on the Small fixture with a config file, a log
// and a Claude Code settings file below a temporary home.
type testEnv struct {
	home     string
	cfg      *config.Config
	db       string
	settings string
	env      map[string]string
	// found is what LookPath returns for total-recall; empty is "not on
	// PATH". exe is the running binary, version what the found one says.
	found, exe, version string
}

func newTestEnv(t *testing.T, extra string) *testEnv {
	t.Helper()
	_, db := fixturedb.Small(t)
	return newTestEnvWith(t, extra, db)
}

// newActivityEnv is newTestEnv on the Activity fixture, whose directories
// resolve to repositories (alpha with a worktree and beta) or not (gamma).
func newActivityEnv(t *testing.T, extra string) *testEnv {
	t.Helper()
	_, db := fixturedb.Activity(t)
	return newTestEnvWith(t, extra, db)
}

func newTestEnvWith(t *testing.T, extra, db string) *testEnv {
	t.Helper()
	home := t.TempDir()
	h := &testEnv{home: home, db: db, settings: filepath.Join(home, ".claude", "settings.json"), env: map[string]string{}}
	h.env["XDG_CONFIG_HOME"] = filepath.Join(home, ".config")
	dir := filepath.Join(home, ".config", "total-recall")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The template keeps its own [backup] table; the overrides go in
	// front of the first table, so they stay top-level keys.
	content := strings.Replace(config.Template, "\n[backup]", "\ndb = "+quote(db)+"\n"+extra+"\n[backup]", 1)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	h.reload(t)
	return h
}

func quote(s string) string { return `"` + s + `"` }

func (h *testEnv) reload(t *testing.T) {
	t.Helper()
	cfg, err := config.Load(config.Options{GOOS: "linux", Home: h.home, Getenv: func(k string) string { return h.env[k] }})
	if err != nil {
		t.Fatal(err)
	}
	h.cfg = cfg
}

// writeSettings writes a Claude Code settings file with hooks for the
// events given (ingest everywhere, the backup on SessionEnd).
func (h *testEnv) writeSettings(t *testing.T, events ...string) {
	t.Helper()
	var parts []string
	for _, ev := range events {
		hooks := `{ "type": "command", "command": "total-recall ingest --hook --detach" }`
		if ev == "SessionEnd" {
			hooks += `, { "type": "command", "command": "total-recall backup --if-older 24h --detach" }`
		}
		parts = append(parts, `"`+ev+`": [ { "hooks": [ `+hooks+` ] } ]`)
	}
	content := `{ "theme": "dark", "hooks": { ` + strings.Join(parts, ", ") + ` } }`
	if err := os.MkdirAll(filepath.Dir(h.settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.settings, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *testEnv) options(t *testing.T) Options {
	t.Helper()
	st, err := store.OpenReadOnly(h.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return Options{
		Config: h.cfg, Store: st, Location: time.UTC, Now: tuitest.Clock, Home: h.home, Version: "1.2.0",
		Getenv:       func(k string) string { return h.env[k] },
		SettingsFile: h.settings,
		ClaudeJSON:   filepath.Join(h.home, ".claude.json"),
		ProjectDir:   h.home,
		LookPath: func(string) (string, error) {
			if h.found == "" {
				return "", os.ErrNotExist
			}
			return h.found, nil
		},
		Executable: func() (string, error) { return h.exe, nil },
		VersionOf:  func(string) (string, error) { return h.version, nil },
	}
}

// drive starts the view with the body size and presses the keys, running
// every command to completion.
func drive(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	msgs := append([]tea.Msg{bodySize}, tuitest.Keys(keys...)...)
	return tuitest.Drive(t, m, msgs...).(Model)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range tuitest.Keys(keys...) {
		m = tuitest.Send(t, m, k).(Model)
	}
	return m
}

func frame(m Model) string { return tuitest.Frame(m) }

func requireContains(t *testing.T, frame string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(frame, w) {
			t.Errorf("frame lacks %q:\n%s", w, frame)
		}
	}
}

func requireLacks(t *testing.T, frame string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if strings.Contains(frame, b) {
			t.Errorf("frame has %q:\n%s", b, frame)
		}
	}
}

// requireRow checks for a status row: label, spaces, then value.
func requireRow(t *testing.T, frame string, label, value string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^ ` + regexp.QuoteMeta(label) + ` +` + regexp.QuoteMeta(value))
	if !re.MatchString(frame) {
		t.Errorf("frame lacks row %q = %q:\n%s", label, value, frame)
	}
}

func (h *testEnv) file(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestStatus shows the archive of the fixture, the schema, the missing
// backup and the errors of the log.
func TestStatus(t *testing.T) {
	h := newTestEnv(t, "")
	log := h.cfg.LogPath()
	if err := os.WriteFile(log, []byte("2026-10-20T09:00:00Z ERROR hook Stop: boom\n2026-10-01T09:00:00Z ERROR old one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := drive(t, New(h.options(t)))
	f := frame(m)
	v := itoa(store.LatestSchemaVersion())
	requireContains(t, f, "[status] settings projects actions setup", "~/.config/total-recall/config.toml",
		"maintenance rebuilds with 16384 and saves space",
		"2026-10-20 09:00  hook Stop: boom", "1 error older")
	requireRow(t, f, "problems", "none")
	requireRow(t, f, "errors", "1 error in the last 24h")
	requireRow(t, f, "archive", "/")
	requireRow(t, f, "size", "0.")
	requireRow(t, f, "page size", "4096")
	requireRow(t, f, "schema", "v"+v+", binary v"+v)
	requireRow(t, f, "sessions", "4")
	requireRow(t, f, "meta records", "0.0 MiB in 1 records")
	requireRow(t, f, "events", "9")
	requireRow(t, f, "sources", "4")
	requireRow(t, f, "last ingest (claude)", "20")
	requireRow(t, f, "last backup", "none  in ~/Documents/total-recall")
	requireRow(t, f, "config", "~/.config/total-recall/config.toml  loaded")
	requireLacks(t, f, "migration pending", h.home, "old one")
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestPages switches with tab, shift+tab and clicks.
func TestPages(t *testing.T) {
	h := newTestEnv(t, "")
	m := drive(t, New(h.options(t)), "tab")
	requireContains(t, frame(m), "status [settings]", "KEY", "VALUE", "ORIGIN")
	m = press(t, m, "tab")
	requireContains(t, frame(m), "[projects]", "no project definitions")
	m = press(t, m, "tab")
	requireContains(t, frame(m), "[actions]", "ACTION", "backup", "doctor")
	m = press(t, m, "tab")
	requireContains(t, frame(m), "[setup]", "CHECK")
	m = press(t, m, "tab")
	requireContains(t, frame(m), "[status]")
	m = press(t, m, "shift+tab")
	requireContains(t, frame(m), "[setup]")
	// A click on a page name.
	line, spans := m.switches()
	x := strings.Index(tuitest.Frame(m), "actions")
	if x < 0 || spans[pageActions].Start > x || spans[pageActions].End <= x {
		t.Fatalf("actions at %d, spans %v, line %q", x, spans, line)
	}
	m = tuitest.Send(t, m, tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft}).(Model)
	requireContains(t, frame(m), "[actions]")
}

// TestSettingsEdit changes values of every kind through the view and
// checks the file.
func TestSettingsEdit(t *testing.T) {
	h := newTestEnv(t, "")
	m := drive(t, New(h.options(t)), "tab")
	// Move to tui.view: down until the key column shows it.
	m = goTo(t, m, "tui.view")
	before := strings.Count(h.file(t), "\n#")
	// An enum: enter takes the next value and writes.
	m = press(t, m, "enter")
	f := frame(m)
	requireContains(t, f, "tui.view written to ~/.config/total-recall/config.toml · takes effect at the next start")
	if got := h.file(t); !strings.Contains(got, "\nview = \"stats\"\n") || strings.Count(got, "\n#") != before {
		t.Errorf("file after enum edit:\n%s", got)
	}
	requireContains(t, f, "view", "stats", "file")
	// A bool toggles and applies at once.
	m = goTo(t, m, "tui.mouse")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "tui.mouse written", "· applied")
	if got := h.file(t); !strings.Contains(got, "\nmouse = false\n") {
		t.Errorf("file after bool edit:\n%s", got)
	}
	// A duration through the input: a bad value is rejected and the file
	// stays, a good one is written.
	m = goTo(t, m, "tui.reload")
	m = press(t, m, "enter")
	if !m.Focused() {
		t.Fatal("input not focused")
	}
	requireContains(t, frame(m), "> 5s", "enter saves, esc cancels")
	m = press(t, m, "backspace", "backspace", "s", "o", "o", "n", "enter")
	requireContains(t, frame(m), "want a duration")
	if !m.Focused() || !strings.Contains(h.file(t), "\nreload = \"5s\"\n") {
		t.Errorf("rejected value changed the file or closed the input:\n%s", h.file(t))
	}
	m = press(t, m, "backspace", "backspace", "backspace", "backspace", "1", "0", "s", "enter")
	requireContains(t, frame(m), "tui.reload written", "· applied")
	if m.Focused() || !strings.Contains(h.file(t), "\nreload = \"10s\"\n") {
		t.Errorf("file after duration edit:\n%s", h.file(t))
	}
	// esc cancels an edit.
	m = press(t, m, "enter", "x", "esc")
	if m.Focused() || !strings.Contains(h.file(t), "\nreload = \"10s\"\n") {
		t.Error("esc did not cancel")
	}
	// d resets to the default: the line is commented out.
	m = press(t, m, "d")
	requireContains(t, frame(m), "tui.reload reset to the default")
	if got := h.file(t); !strings.Contains(got, "\n# reload = \"10s\"\n") {
		t.Errorf("file after reset:\n%s", got)
	}
	m = press(t, m, "d")
	requireContains(t, frame(m), "tui.reload is not set in the file")
	// A table cannot be edited here.
	m = goTo(t, m, "accounts")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "accounts is a table: edit it in the file")
	// The backup of the file exists.
	if _, err := os.Stat(h.cfg.Path + ".bak"); err != nil {
		t.Error("no .bak")
	}
}

// goTo moves the mark of the settings page to key.
func goTo(t *testing.T, m Model, key string) Model {
	t.Helper()
	p := m.pages[pageSettings].(settingsPage)
	for i, r := range p.rows {
		if r.key == key {
			p.row = i
			m.pages = m.replace(pageSettings, p)
			return m
		}
	}
	t.Fatalf("no row %q", key)
	return m
}

// TestSettingsOverride refuses to edit a value the environment sets.
func TestSettingsOverride(t *testing.T) {
	h := newTestEnv(t, "")
	h.env[config.EnvBackupDir] = filepath.Join(h.home, "elsewhere")
	h.reload(t)
	m := drive(t, New(h.options(t)), "tab")
	m = goTo(t, m, "backup.dir")
	requireContains(t, frame(m), "~/elsewhere", "env")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "backup.dir is set by env; a value in the file would not apply")
	if m.Focused() {
		t.Error("input opened")
	}
}

// TestActionsBackupSameDay: with a copy of the day the action asks for and
// writes a further one, and leaves the first alone.
func TestActionsBackupSameDay(t *testing.T) {
	h := newTestEnv(t, "")
	if err := os.MkdirAll(h.cfg.Backup.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(h.cfg.Backup.Dir, "archive-2026-10-20.db.gz")
	if err := os.WriteFile(first, []byte("the copy of the morning"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := drive(t, New(h.options(t)), "tab", "tab", "tab")
	next, err := store.NextBackupName(h.cfg.Backup.Dir, m.opts.Now().In(h.cfg.Location()))
	if err != nil || !strings.HasPrefix(next, "archive-2026-10-20-") {
		t.Fatalf("NextBackupName = %q %v", next, err)
	}
	m = press(t, m, "enter")
	requireContains(t, frame(m), "backup: write "+next+" to ~/Documents/total-recall")
	m = press(t, m, "y")
	requireContains(t, frame(m), "backup: backup written: "+next)
	if b, err := os.ReadFile(first); err != nil || string(b) != "the copy of the morning" {
		t.Errorf("first copy: %q %v", b, err)
	}
	m = press(t, m, "shift+tab", "shift+tab", "shift+tab")
	requireRow(t, frame(m), "last backup", "~/Documents/total-recall/"+next)
}

func TestRetentionText(t *testing.T) {
	tests := []struct {
		daily, monthly int
		want           string
	}{
		{7, 0, "keep 7 daily and all monthly backups"},
		{7, 3, "keep 7 daily and 3 monthly backups"},
		{0, 1, "keep 0 daily and 1 monthly backups"},
	}
	for _, tt := range tests {
		if got := retentionText(tt.daily, tt.monthly); got != tt.want {
			t.Errorf("retentionText(%d, %d) = %q, want %q", tt.daily, tt.monthly, got, tt.want)
		}
	}
}

// TestActionsConfirm: a writing action asks first, esc cancels and
// nothing is written, y runs it; a reading one runs right away.
func TestActionsConfirm(t *testing.T) {
	h := newTestEnv(t, "")
	m := drive(t, New(h.options(t)), "tab", "tab", "tab")
	requireContains(t, frame(m), "▸ backup", "write a compressed copy to ~/Documents/total-recall, keep 7 daily and a")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "backup: write archive-2026-10-20.db.zst to ~/Documents/total-recall and prune older backups", "y confirms, esc cancels")
	m = press(t, m, "esc")
	requireContains(t, frame(m), "backup: cancelled")
	if _, err := os.Stat(h.cfg.Backup.Dir); err == nil {
		t.Error("backup directory created although cancelled")
	}
	m = press(t, m, "enter", "y")
	f := frame(m)
	requireContains(t, f, "backup: backup written: archive-2026-10-20.db.zst", "writing archive-2026-10-20.db.zst")
	if _, err := os.Stat(filepath.Join(h.cfg.Backup.Dir, "archive-2026-10-20.db.zst")); err != nil {
		t.Errorf("no backup: %v", err)
	}
	if m.busy {
		t.Error("still busy")
	}
	// The status knows the backup now.
	m = press(t, m, "shift+tab", "shift+tab", "shift+tab")
	requireRow(t, frame(m), "last backup", "~/Documents/total-recall/archive-2026-10-20.db.zst")
	// A dry run needs no confirmation.
	m = press(t, m, "tab", "tab", "tab", "down", "enter")
	requireContains(t, frame(m), "maintenance (dry run): would be rebuilt:", "page size 4096 → 16384")
	// migrate is disabled: nothing pending.
	m = press(t, m, "down", "down", "enter")
	requireContains(t, frame(m), "migrate: nothing pending", "schema v"+itoa(store.LatestSchemaVersion())+" is current")
	// doctor reads the transcripts of the fixture.
	m = press(t, m, "down", "down", "down", "down", "down", "enter")
	requireContains(t, frame(m), "doctor: clean", "harness claude: 0 sources")
	// reindex dry run.
	m = press(t, m, "up", "up", "up", "enter")
	requireContains(t, frame(m), "reindex actors (dry run): reindex --actors (dry run): 4 user messages")
}

// TestSetup checks the installation with synthetic files.
func TestSetup(t *testing.T) {
	h := newTestEnv(t, "")
	h.writeSettings(t, "SessionStart", "Stop", "SubagentStop")
	h.found, h.exe, h.version = filepath.Join(h.home, "bin", "total-recall"), filepath.Join(h.home, "bin", "total-recall"), "1.2.0"
	m := drive(t, New(h.options(t)), "shift+tab")
	f := frame(m)
	requireContains(t, f,
		"config      √ ~/.config/total-recall/config.toml loaded",
		"hooks       × ~/.claude/settings.json lacks SessionEnd; add the hook block (c copies it)",
		"mcp         × not registered with Claude Code (optional); c copies the claude mcp add command",
		"binary      √ ~/bin/total-recall on PATH, this version (1.2.0)", // the fake paths do not exist, so VersionOf decides
		"backups     × ~/Documents/total-recall does not exist  enter: create the directory",
		"transcripts × ~/.claude/projects not found",
		"archive     √ /",
		"hook block for ~/.claude/settings.json; c copies it",
		`"SessionEnd": [`, `"command": "total-recall backup --if-older 24h --detach"`)
	requireLacks(t, f, h.home)
	// The fix of the backup directory after a confirmation.
	m = press(t, m, "down", "down", "down", "down", "enter")
	requireContains(t, frame(m), "backups: create the directory? y confirms, esc cancels")
	m = press(t, m, "esc")
	if _, err := os.Stat(h.cfg.Backup.Dir); err == nil {
		t.Error("created although cancelled")
	}
	m = press(t, m, "enter", "y")
	requireContains(t, frame(m), "backups: done", "backups     ! ~/Documents/total-recall exists, no backup yet (actions: backup)")
	if fi, err := os.Stat(h.cfg.Backup.Dir); err != nil || !fi.IsDir() {
		t.Errorf("backup dir: %v", err)
	}
	// c copies the block (a raw terminal write).
	m = press(t, m, "c")
	requireContains(t, frame(m), "hook block copied to the clipboard")
	// All hooks present, a different version on PATH.
	h.writeSettings(t, "SessionStart", "Stop", "SubagentStop", "SessionEnd")
	h.version = "1.1.0"
	m2, _ := m.pages[pageSetup].reload(&m)
	m.pages = m.replace(pageSetup, m2)
	m = tuitest.Run(t, m, func() tea.Msg { return nil }).(Model)
	p, cmd := m.pages[pageSetup].reload(&m)
	m.pages = m.replace(pageSetup, p)
	m = tuitest.Run(t, m, cmd).(Model)
	requireContains(t, frame(m),
		"hooks       √ ~/.claude/settings.json: SessionStart, Stop, SubagentStop, SessionEnd",
		"binary      ! ~/bin/total-recall on PATH is 1.1.0, this is 1.2.0")
}

// TestSetupWritesTemplate: without a config file the setup offers the
// template and the view reloads the configuration.
func TestSetupWritesTemplate(t *testing.T) {
	h := newTestEnv(t, "")
	if err := os.Remove(h.cfg.Path); err != nil {
		t.Fatal(err)
	}
	h.reload(t)
	if h.cfg.Loaded {
		t.Fatal("loaded")
	}
	m := drive(t, New(h.options(t)), "shift+tab")
	requireContains(t, frame(m), "config      × ~/.config/total-recall/config.toml missing (defaults apply)  enter: write template")
	m = press(t, m, "enter", "y")
	requireContains(t, frame(m), "config      √ ~/.config/total-recall/config.toml loaded")
	if !m.opts.Config.Loaded {
		t.Error("configuration not reloaded")
	}
	if b, err := os.ReadFile(h.cfg.Path); err != nil || !strings.HasPrefix(string(b), "# total-recall configuration") {
		t.Errorf("template not written: %v", err)
	}
}

// TestMigrate: an outdated archive shows the pending migration on the
// status page, disables the writing actions except backup, and migrate
// runs after a confirmation with the plan shown first.
func TestMigrate(t *testing.T) {
	h := newTestEnv(t, "")
	old := filepath.Join(h.home, "old", "archive.db")
	if err := store.CreateAtVersion(old, 1); err != nil {
		t.Fatal(err)
	}
	h.db = old
	if err := os.WriteFile(h.cfg.Path, []byte(strings.Replace(config.Template, "\n[backup]", "\ndb = "+quote(old)+"\n[backup]", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	h.reload(t)
	m := drive(t, New(h.options(t)))
	f := frame(m)
	latest := store.LatestSchemaVersion()
	requireRow(t, f, "schema", "v1, binary v"+itoa(latest)+"  migration pending: back up, then run migrate (actions)")
	requireRow(t, f, "raw", "unknown until the migration ran")
	m = press(t, m, "shift+tab", "shift+tab")
	f = frame(m)
	requireContains(t, f, "maintenance (dry run)", "· migrate first", "ingest all", "no backup yet, run backup first")
	requireLacks(t, f, "· nothing pending")
	// The plan comes with the confirmation.
	m = press(t, m, "down", "down", "down", "enter")
	f = frame(m)
	requireContains(t, f, "migrate: migrate from schema v1 to v"+itoa(latest)+" after a copy (archive.db.pre-v1.bak)",
		"y confirms, esc cancels", "schema    v1 -> v"+itoa(latest)+", "+itoa(latest-1)+" pending migrations:", "v2    sessions: harness-generated title", "backup    ", "space     about")
	m = press(t, m, "esc")
	requireContains(t, frame(m), "migrate: cancelled")
	if _, err := os.Stat(old + ".pre-v1.bak"); err == nil {
		t.Error("copy written although cancelled")
	}
	m = press(t, m, "enter", "y")
	f = frame(m)
	requireContains(t, f, "migrate: migrated v1 -> v"+itoa(latest)+"; the copy archive.db.pre-v1.bak can be removed once everything works")
	if _, err := os.Stat(old + ".pre-v1.bak"); err != nil {
		t.Errorf("no pre-migration copy: %v", err)
	}
	st, err := store.OpenReadOnly(old)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if st.MigrationPending() {
		t.Error("still pending")
	}
	// The status is loaded again after a writing action: nothing pending.
	requireContains(t, frame(m), "schema v"+itoa(latest)+" is current · nothing pending")
	m = press(t, m, "tab", "tab")
	requireRow(t, frame(m), "schema", "v"+itoa(latest)+", binary v"+itoa(latest))
}

// TestNoConfig: the view without a configuration shows hints and takes
// no keys badly.
func TestNoConfig(t *testing.T) {
	m := drive(t, New(Options{Now: tuitest.Clock}), "tab", "enter", "tab", "tab", "enter", "y", "tab", "enter", "c")
	requireContains(t, frame(m), "no configuration")
}

// TestSetKeys: new bindings act at once; the page stays.
func TestSetKeys(t *testing.T) {
	h := newTestEnv(t, "")
	km, err := keys.Resolve(map[string][]string{keys.ConfigNext: {"n"}})
	if err != nil {
		t.Fatal(err)
	}
	m := drive(t, New(h.options(t)), "tab")
	m = m.SetKeys(km).(Model)
	if m.active != pageSettings {
		t.Fatalf("page = %d after SetKeys", m.active)
	}
	if m = press(t, m, "tab"); m.active != pageSettings {
		t.Error("tab still switches the page")
	}
	if m = press(t, m, "n"); m.active != pageProjects {
		t.Errorf("n does not switch the page: %d", m.active)
	}
	if h := m.keys.Pages.Help(); h.Key != "n" {
		t.Errorf("pages help = %q", h.Key)
	}
}

// TestSettingsEffect: the default line of the marked key says what a
// change does, and the notice after a write says the same.
func TestSettingsEffect(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"tui.reload", "applied at once"},
		{"tui.mouse", "applied at once"},
		{"tui.activity_projects", "applied at once"},
		{"tui.activity_newest", "applied at once"},
		{"day_start", "applied at once"},
		{"work.target", "applied at once"},
		{"work.bundesland", "applied at once"},
		{"keys.global_quit", "applied at once"},
		{"project", "applied at once"},
		{"db", "takes effect at the next start"},
		{"timezone", "takes effect at the next start"},
		{"tui.view", "takes effect at the next start"},
		{"tui.stats_period", "takes effect at the next start"},
		{"tui.activity_window", "takes effect at the next start"},
		{"backup.dir", "used by the next command or action"},
		{"redact.enabled", "used by the next command or action"},
		{"chart.scale", "used by the next command or action"},
		{"chart.lang", "used by the next command or action"},
		{"worktime.max_gap", "used by the next command or action"},
		{"accounts", "used by the next command or action"},
	}
	h := newTestEnv(t, "")
	m := drive(t, New(h.options(t)), "tab")
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := goTo(t, m, tt.key)
			requireContains(t, frame(m), "· change: "+tt.want)
		})
	}
	// The notice after a write and after a reset.
	m = goTo(t, m, "work.target")
	m = press(t, m, "enter", "backspace", "backspace", "4", "h", "enter")
	requireContains(t, frame(m), "work.target written to ~/.config/total-recall/config.toml · applied at once")
	m = press(t, m, "d")
	requireContains(t, frame(m), "work.target reset to the default · applied at once")
}

// TestSetupMCP: the mcp line reads only the mcpServers key of ~/.claude.json
// and .mcp.json, shows and copies the entry, and writes nothing.
func TestSetupMCP(t *testing.T) {
	h := newTestEnv(t, "")
	h.found, h.exe, h.version = filepath.Join(h.home, "bin", "total-recall"), filepath.Join(h.home, "bin", "total-recall"), "1.2.0"
	if err := os.MkdirAll(filepath.Dir(h.found), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.found, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeJSON := filepath.Join(h.home, ".claude.json")
	other := `{"oauthAccount": {"emailAddress": "secret@example.invalid"}, "mcpServers": {"other": {"command": "x", "args": ["serve"]}}}`
	if err := os.WriteFile(claudeJSON, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	m := drive(t, New(h.options(t)), "shift+tab", "down", "down")
	f := frame(m)
	requireContains(t, f, "mcp         × not registered with Claude Code (optional)",
		"MCP server entry for Claude Code, nothing is written; c copies the command",
		"claude mcp add --scope user total-recall -- total-recall mcp", `"mcpServers": {`, `"args": [`)
	requireLacks(t, f, "secret@example.invalid", "oauthAccount")
	m = press(t, m, "c")
	requireContains(t, frame(m), "claude mcp add command copied to the clipboard")

	// Registered with an absolute path in ~/.claude.json.
	entry := fmt.Sprintf(`{"oauthAccount": {"emailAddress": "secret@example.invalid"}, "mcpServers": {"tr": {"type": "stdio", "command": %q, "args": ["mcp"]}}}`, h.found)
	if err := os.WriteFile(claudeJSON, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	m = reloadSetup(t, m)
	f = frame(m)
	requireContains(t, f, "mcp         √ ~/.claude.json: total-recall mcp")
	requireLacks(t, f, "secret@example.invalid")

	// Registered in the project's .mcp.json, command missing.
	if err := os.Remove(claudeJSON); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(h.home, "gone", "total-recall")
	if err := os.WriteFile(filepath.Join(h.home, ".mcp.json"), []byte(fmt.Sprintf(`{"mcpServers": {"total-recall": {"command": %q, "args": ["mcp"]}}}`, gone)), 0o600); err != nil {
		t.Fatal(err)
	}
	m = reloadSetup(t, m)
	requireContains(t, frame(m), "mcp         ! ~/.mcp.json: total-recall mcp, command ~/gone/total-recall does not exist")
	// Nothing was written to the Claude files.
	if _, err := os.Stat(claudeJSON); err == nil {
		t.Error("~/.claude.json was written")
	}
}

func reloadSetup(t *testing.T, m Model) Model {
	t.Helper()
	p, cmd := m.pages[pageSetup].reload(&m)
	m.pages = m.replace(pageSetup, p)
	return tuitest.Run(t, m, cmd).(Model)
}
