package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// betaSession is the fixture session the query "release" hits first.
const betaSession = "claude:" + testfixture.ActivitySessionBeta1

// reloadHarness is an app on a read-only store of the Activity fixture
// with a writer beside it (the hooks), a clock the test moves and a
// ticker that records what it was asked to schedule instead of firing.
type reloadHarness struct {
	w     *store.Store // the writer
	path  string
	now   time.Time
	ticks []time.Duration
	// off builds the app with the reload switched off.
	off bool
}

func newReloadHarness(t *testing.T) *reloadHarness {
	t.Helper()
	w, path := fixturedb.Activity(t)
	return &reloadHarness{w: w, path: path, now: tuitest.Now}
}

// app builds the app on a fresh read-only store.
func (h *reloadHarness) app(t *testing.T, opts Options) App {
	t.Helper()
	r, err := store.OpenReadOnly(h.path, store.WithLocation(testfixture.ActivityLocation))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	opts.Store, opts.Location = r, testfixture.ActivityLocation
	opts.Now = func() time.Time { return h.now }
	opts.Tick = func(d time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		h.ticks = append(h.ticks, d)
		return nil
	}
	if opts.Reload == 0 && !h.off {
		opts.Reload = 5 * time.Second
	}
	app, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// write adds one human message to session at ts, as a hook would.
func (h *reloadHarness) write(t *testing.T, session, id string, ts time.Time, text string) {
	t.Helper()
	ev := model.Event{ID: "claude:" + id, SessionID: session, TS: ts, Kind: model.KindMessage, Role: "user", Actor: model.ActorHuman, Text: text, SourcePath: "/src/live.jsonl"}
	if err := h.w.WriteBatch(context.Background(), store.SourceState{}, []model.Session{{ID: session, Harness: "claude"}}, []model.Event{ev}); err != nil {
		t.Fatal(err)
	}
}

// tick lets the interval pass: the app checks the archive and, on a
// change, reloads; every command runs to completion.
func (h *reloadHarness) tick(t *testing.T, m tea.Model, d time.Duration) tea.Model {
	t.Helper()
	h.now = h.now.Add(d)
	return tuitest.Send(t, m, tickMsg{})
}

func frame(m tea.Model) string { return tuitest.Frame(m) }

// start sends the window size and runs Init.
func start(t *testing.T, app App, keys ...string) tea.Model {
	t.Helper()
	msgs := append([]tea.Msg{tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}}, tuitest.Keys(keys...)...)
	return tuitest.Drive(t, app, msgs...)
}

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

// TestReloadSession: a new event of the open session appears after the
// interval, the marked event stays marked, the header shows the time.
func TestReloadSession(t *testing.T) {
	h := newReloadHarness(t)
	m := start(t, h.app(t, Options{Query: "release"}), "enter")
	f := frame(m)
	requireContains(t, f, "1/2 events", "20:30:00  human ◄", "updated 13:00:00")
	requireLacks(t, f, "ship it")

	h.write(t, betaSession, "live-1", time.Date(2025, 12, 4, 20, 31, 0, 0, testfixture.ActivityLocation), "ship it")
	m = h.tick(t, m, 5*time.Second)
	f = frame(m)
	requireContains(t, f, "1/3 events", "20:30:00  human ◄", "ship it", "updated 13:00:05")
	requireLacks(t, f, "reload:")
}

// TestReloadFollowsBottom: a transcript scrolled to the end stays at the
// end when events are appended, one scrolled up keeps its line.
func TestReloadFollowsBottom(t *testing.T) {
	h := newReloadHarness(t)
	long := strings.Repeat("line\n", 40)
	for i := 0; i < 3; i++ {
		h.write(t, betaSession, "long-"+string(rune('a'+i)), time.Date(2025, 12, 4, 20, 32+i, 0, 0, testfixture.ActivityLocation), long)
	}
	tests := []struct {
		name string
		keys []string
		want []string
	}{
		{"at bottom follows", []string{"G"}, []string{"appended", "bot"}},
		{"at top stays", []string{"g"}, []string{"1/6 events", "top", "tag the release"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := start(t, h.app(t, Options{Query: "release"}), append([]string{"enter"}, tt.keys...)...)
			h.write(t, betaSession, "app-"+tt.name[:2], time.Date(2025, 12, 4, 20, 40, 0, 0, testfixture.ActivityLocation), "appended "+tt.name)
			m = h.tick(t, m, 5*time.Second)
			requireContains(t, frame(m), tt.want...)
		})
	}
}

// TestReloadSearchMark: the search runs again with the same query; the
// mark stays on the same hit even when a new hit sorts above it.
func TestReloadSearchMark(t *testing.T) {
	h := newReloadHarness(t)
	m := start(t, h.app(t, Options{Query: "release"}), "tab", "j")
	before := frame(m)
	requireContains(t, before, "▸")
	marked := markedLine(before)

	h.write(t, betaSession, "live-2", time.Date(2025, 12, 4, 20, 31, 0, 0, testfixture.ActivityLocation), "release the release")
	m = h.tick(t, m, 5*time.Second)
	after := frame(m)
	requireContains(t, after, "release the release")
	if got := markedLine(after); got != marked {
		t.Errorf("marked hit moved from %q to %q:\n%s", marked, got, after)
	}
	if !strings.Contains(after, "> release") {
		t.Errorf("input changed:\n%s", after)
	}
}

// markedLine returns the text line below the hit marked with ▸, without
// the preview column.
func markedLine(frame string) string {
	lines := strings.Split(frame, "\n")
	for i, l := range lines {
		if strings.Contains(l, "▸") && i+1 < len(lines) {
			text, _, _ := strings.Cut(lines[i+1], "│")
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// TestReloadUnchanged: without a change nothing reloads: the time in the
// header stays and no view goes stale.
func TestReloadUnchanged(t *testing.T) {
	h := newReloadHarness(t)
	m := start(t, h.app(t, Options{Query: "release"}))
	m = h.tick(t, m, 5*time.Second)
	m = h.tick(t, m, 5*time.Second)
	requireContains(t, frame(m), "updated 13:00:00")
	for i, s := range m.(App).stale {
		if s {
			t.Errorf("view %d stale without a change", i)
		}
	}
	if len(h.ticks) != 3 {
		t.Errorf("ticks scheduled = %d, want 3 (start and after each check)", len(h.ticks))
	}
}

// TestReloadStale: a change while the stats are shown reloads the stats
// and marks the search stale; switching back reloads it then.
func TestReloadStale(t *testing.T) {
	h := newReloadHarness(t)
	m := start(t, h.app(t, Options{Query: "release", Start: ViewStats}))
	requireContains(t, frame(m), "No activity")
	h.write(t, betaSession, "live-3", h.now.Add(-24*time.Hour), "release today")
	m = h.tick(t, m, 5*time.Second)
	f := frame(m)
	requireContains(t, f, "beta", "updated 13:00:05")
	requireLacks(t, f, "No activity")
	a := m.(App)
	if !a.stale[0] || a.stale[1] || !a.stale[2] {
		t.Errorf("stale = %v, want search and activity stale", a.stale)
	}
	m = tuitest.Send(t, m, tuitest.Key("1"))
	f = frame(m)
	requireContains(t, f, "release today")
	if a := m.(App); a.stale[0] {
		t.Error("search still stale after it was shown")
	}
}

// TestReloadOff: reload = 0 schedules no check; the reload key still
// loads on demand.
func TestReloadOff(t *testing.T) {
	h := newReloadHarness(t)
	h.off = true
	m := start(t, h.app(t, Options{Query: "release"}), "tab")
	if len(h.ticks) != 0 {
		t.Fatalf("ticks scheduled with reload off: %v", h.ticks)
	}
	h.write(t, betaSession, "live-4", time.Date(2025, 12, 4, 20, 31, 0, 0, testfixture.ActivityLocation), "release manually")
	h.now = h.now.Add(time.Minute)
	requireLacks(t, frame(m), "release manually")
	m = tuitest.Send(t, m, tuitest.Key("R"))
	requireContains(t, frame(m), "release manually", "updated 13:01:00")
	if len(h.ticks) != 0 {
		t.Errorf("the reload key scheduled a check: %v", h.ticks)
	}
}

// TestReloadInterval: the configured interval is what gets scheduled.
func TestReloadInterval(t *testing.T) {
	h := newReloadHarness(t)
	start(t, h.app(t, Options{Reload: 2 * time.Second}))
	if len(h.ticks) != 1 || h.ticks[0] != 2*time.Second {
		t.Errorf("ticks = %v, want [2s]", h.ticks)
	}
}

// TestReloadReplaced: maintenance swaps the file; the next check reopens
// it and later writes to the new file are picked up.
func TestReloadReplaced(t *testing.T) {
	h := newReloadHarness(t)
	m := start(t, h.app(t, Options{Query: "release"}), "enter")
	requireContains(t, frame(m), "1/2 events")
	if err := h.w.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(h.path)
	res, err := store.Maintain(context.Background(), h.path, store.MaintainOptions{
		PageSize: 16384, LockDir: filepath.Join(dir, "locks"), Now: h.now,
	})
	if err != nil || !res.Swapped {
		t.Fatalf("Maintain = %+v, %v", res, err)
	}
	w2, err := store.Open(h.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w2.Close() })
	h.w = w2
	h.write(t, betaSession, "live-5", time.Date(2025, 12, 4, 20, 31, 0, 0, testfixture.ActivityLocation), "after the swap")

	m = h.tick(t, m, 5*time.Second)
	f := frame(m)
	requireContains(t, f, "1/3 events", "after the swap", "updated 13:00:05")
	requireLacks(t, f, "reload:")
	h.write(t, betaSession, "live-6", time.Date(2025, 12, 4, 20, 32, 0, 0, testfixture.ActivityLocation), "and once more")
	m = h.tick(t, m, 5*time.Second)
	requireContains(t, frame(m), "1/4 events", "and once more", "updated 13:00:10")
}

// TestHeaderMigrationPending: the header says loudly that the archive needs
// a migration, in place of the reload time.
func TestHeaderMigrationPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	if err := store.CreateAtVersion(path, 1); err != nil {
		t.Fatal(err)
	}
	r, err := store.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	app, err := New(Options{Store: r, Now: func() time.Time { return tuitest.Now }})
	if err != nil {
		t.Fatal(err)
	}
	app.width = tuitest.Width
	h := ansi.Strip(app.header())
	if !strings.Contains(h, "migration pending: total-recall migrate") || strings.Contains(h, "updated") {
		t.Errorf("header = %q", h)
	}
}
