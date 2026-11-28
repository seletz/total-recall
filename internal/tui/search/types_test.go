package search

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/seletz/total-recall/internal/model"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// demoModel is the view over the demo archive, sized like the tests of the
// other views; the catalog is loaded like the app does on start.
func demoModel(t *testing.T, query string) Model {
	t.Helper()
	st, _ := fixturedb.Demo(t)
	m := New(Options{Searcher: st, Loader: st, Projects: st, Resolver: project.Resolver{}, Query: query,
		Now: func() time.Time { return demo.Now }, Location: demo.Location, Debounce: time.Nanosecond, PreviewDebounce: time.Nanosecond})
	return start(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
}

// start initialises the view (catalog and the search of the initial
// query, see tuitest.Drive) and sends msgs.
func start(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	return tuitest.Drive(t, m, msgs...).(Model)
}

// drive sends msgs to a started view, running the commands of each to
// completion.
func drive(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	var tm tea.Model = m
	for _, msg := range msgs {
		tm = tuitest.Send(t, tm, msg)
	}
	return tm.(Model)
}

func keysOf(names ...string) []tea.Msg { return tuitest.Keys(names...) }

// moveTo marks the first hit of type typ, moving down from the mark; it
// fails when there is none.
func moveTo(t *testing.T, m Model, typ store.HitType) Model {
	t.Helper()
	for range m.hits {
		if h := m.marked(); h != nil && h.Type == typ {
			return m
		}
		m = drive(t, m, keysOf("j")...)
	}
	t.Fatalf("no %s hit in the list", typ)
	return m
}

func groupOf(m Model, t store.HitType) *store.TypeHits {
	for i := range m.groups {
		if m.groups[i].Type == t {
			return &m.groups[i]
		}
	}
	return nil
}

// TestTypeSwitcher: event is the default; keys and clicks toggle types, the
// selection keeps the display order, and the last type cannot be switched
// off.
func TestTypeSwitcher(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want []store.HitType
	}{
		{"default", nil, []store.HitType{store.TypeEvent}},
		{"add session", []string{"tab", "s"}, []store.HitType{store.TypeEvent, store.TypeSession}},
		{"add memory then session", []string{"tab", "m", "s"}, []store.HitType{store.TypeEvent, store.TypeSession, store.TypeMemory}},
		{"swap event for memory", []string{"tab", "m", "e"}, []store.HitType{store.TypeMemory}},
		{"last type stays", []string{"tab", "e"}, []store.HitType{store.TypeEvent}},
		{"keys are text in the input", []string{"s", "m"}, []store.HitType{store.TypeEvent}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, demoModel(t, demo.Query), keysOf(tt.keys...)...)
			if !slicesEqual(m.types, tt.want) {
				t.Errorf("types = %v, want %v", m.types, tt.want)
			}
			if len(m.groups) != len(tt.want) {
				t.Errorf("%d groups, want %d", len(m.groups), len(tt.want))
			}
		})
	}
}

func slicesEqual(a, b []store.HitType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ctrlClick is a left click with ctrl held.
func ctrlClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl}
}

// TestTypeClick: a click on a type in the type row selects only that type
// (#376), a click with ctrl (or shift, alt) adds it to the selection or
// drops it, the last type stays; a click beside the names does nothing,
// and the status line says how to add.
func TestTypeClick(t *testing.T) {
	m := demoModel(t, demo.Query)
	_, z := m.typeRow(m.width)
	if len(z) != len(searchableTypes()) || len(z) != 10 {
		t.Fatalf("%d type zones, want 10", len(z))
	}
	idx := map[store.HitType]int{}
	for i, ty := range searchableTypes() {
		idx[ty] = i
	}
	m = drive(t, m, click(z[idx[store.TypeSession]].Start, 1))
	if !slicesEqual(m.types, []store.HitType{store.TypeSession}) {
		t.Errorf("after clicking session: types = %v, want only session", m.types)
	}
	if !strings.Contains(m.status(), "ctrl+click adds") {
		t.Errorf("status after a click lacks the hint: %q", m.status())
	}
	_, z = m.typeRow(m.width)
	m = drive(t, m, ctrlClick(z[idx[store.TypeMemory]].Start, 1))
	if !slicesEqual(m.types, []store.HitType{store.TypeSession, store.TypeMemory}) {
		t.Errorf("after ctrl+click on memory: types = %v", m.types)
	}
	_, z = m.typeRow(m.width)
	m = drive(t, m, click(z[idx[store.TypeMemory]].Start, 1))
	if !slicesEqual(m.types, []store.HitType{store.TypeMemory}) {
		t.Errorf("plain click on a selected type among two: types = %v, want only memory", m.types)
	}
	_, z = m.typeRow(m.width)
	m = drive(t, m, ctrlClick(z[idx[store.TypeMemory]].Start, 1))
	if !slicesEqual(m.types, []store.HitType{store.TypeMemory}) {
		t.Errorf("ctrl+click on the last type: types = %v, want it to stay", m.types)
	}
	before := len(m.groups)
	_, z = m.typeRow(m.width)
	m = drive(t, m, click(z[len(z)-1].End+1, 1))
	if len(m.groups) != before || len(m.types) != 1 {
		t.Errorf("a click beside the names changed the selection: %v", m.types)
	}
}

// TestTypeRow: every type shows its key and its state: the selected ones
// in brackets. A narrow row drops the keys.
func TestTypeRow(t *testing.T) {
	m := demoModel(t, demo.Query)
	row, _ := m.typeRow(m.width)
	for _, want := range []string{"[e event]", "u input", "g answer", "c tool_call", "o tool_result", "I image", "D document", "f file", "s session", "m memory"} {
		if !strings.Contains(ansi.Strip(row), want) {
			t.Errorf("row lacks %q: %q", want, ansi.Strip(row))
		}
	}
	narrow, spans := m.typeRow(70)
	if strings.Contains(ansi.Strip(narrow), "g answer") || !strings.Contains(ansi.Strip(narrow), "[event]") || len(spans) == 0 {
		t.Errorf("narrow row = %q", ansi.Strip(narrow))
	}
}

// TestGroupedList: with several types the hits are listed in one group per
// type, each with a header, in the format of its type; with one type there
// are no headers.
func TestGroupedList(t *testing.T) {
	m := demoModel(t, demo.Query)
	for _, r := range m.rows {
		if r.header {
			t.Fatal("a header with only the event type selected")
		}
	}
	m = drive(t, m, keysOf("tab", "s", "m")...)
	f := tuitest.Frame(m)
	var headers []string
	for _, r := range m.rows {
		if r.header {
			headers = append(headers, string(m.groups[r.group].Type))
		}
	}
	if strings.Join(headers, ",") != "event,session,memory" {
		t.Fatalf("headers = %v, want one per type", headers)
	}
	for _, want := range []string{" event  10", " session  0"} {
		if !strings.Contains(f, want) {
			t.Errorf("frame lacks %q:\n%s", want, f)
		}
	}
	if g := groupOf(m, store.TypeMemory); g == nil || len(g.Hits) != 1 || g.Hits[0].Memory == nil {
		t.Errorf("memory group = %+v", g)
	}
}

// TestSessionAndMemoryFormats: the hit of a session shows its name and
// size, the hit of a memory file its project and path.
func TestSessionAndMemoryFormats(t *testing.T) {
	m := demoModel(t, "release")
	m = drive(t, m, keysOf("tab", "s", "m", "e")...)
	f := tuitest.Frame(m)
	for _, want := range []string{"Release v0.3.0", " events  kompass", "release-process.md"} {
		if !strings.Contains(f, want) {
			t.Errorf("frame lacks %q:\n%s", want, f)
		}
	}
}

// TestViewers: the preview shows the viewer of the marked hit's type.
func TestViewers(t *testing.T) {
	tests := []struct {
		name  string
		query string
		keys  []string
		want  []string
	}{
		{"memory file", demo.Query, []string{"tab", "m", "e"},
			[]string{"release-process.md", "kompass  claude", "A release is a signed tag on main.", "1. Update the changelog", "Steps"}},
		{"tool call", "cask generated", nil,
			[]string{"tool_use Bash ◄"[:13], "brew info --cask --json=v2 mara/tap/kompass", "tool (tool_result) ◄", `"casks": [`, `    "token": "kompass",`}},
		{"session", "release", []string{"tab", "s", "e"},
			[]string{"Release v0.3.0", "events (", "Lade den Homebrew-Cask mit goreleaser"}},
		{"message", demo.Query, nil,
			[]string{"Lade den Homebrew-Cask", "◄"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, demoModel(t, tt.query), keysOf(tt.keys...)...)
			f := tuitest.Frame(m)
			for _, want := range tt.want {
				if !strings.Contains(f, want) {
					t.Errorf("frame lacks %q:\n%s", want, f)
				}
			}
		})
	}
}

// TestPin: p pins the project of the marked hit (events, sessions and
// memory alike), the pin shows in the rule row and restricts every type; p
// again, the sign or "all projects" drops it.
func TestPin(t *testing.T) {
	m := demoModel(t, "tests")
	m = drive(t, m, keysOf("tab", "s", "m")...)
	all := len(m.hits)
	m = drive(t, m, keysOf("p")...) // on the first event hit
	if m.pin == nil || m.pin.name != "kompass" {
		t.Fatalf("pin = %+v, want kompass", m.pin)
	}
	if f := tuitest.Frame(m); !strings.Contains(f, "project: kompass ✕") {
		t.Errorf("rule row lacks the pin:\n%s", f)
	}
	for _, h := range m.hits {
		if name := m.projectName(h); name != "kompass" {
			t.Errorf("hit of project %q survived the pin", name)
		}
	}
	if len(m.hits) >= all {
		t.Errorf("pin did not narrow the list: %d of %d hits", len(m.hits), all)
	}
	m = drive(t, m, keysOf("p")...)
	if m.pin != nil || len(m.hits) != all {
		t.Errorf("p did not drop the pin: pin %+v, %d of %d hits", m.pin, len(m.hits), all)
	}

	// The sign drops it as well, and a memory hit pins its memory project.
	m = drive(t, m, keysOf("e", "j")...) // memory and session only, on a hit
	m = drive(t, m, keysOf("p")...)
	if m.pin == nil {
		t.Fatal("no pin from the marked hit")
	}
	left, right := m.columns()
	_, z := m.ruleRow(left, right)
	if z.clear.Start >= z.clear.End {
		t.Fatalf("no sign to drop the pin: %+v", z)
	}
	m = drive(t, m, click(z.clear.Start, 2))
	if m.pin != nil {
		t.Error("the sign did not drop the pin")
	}
}

// TestProjectPicker: P lists the projects of the archive, enter pins the
// one under the mark, "all projects" drops the pin, esc leaves everything
// as it was.
func TestProjectPicker(t *testing.T) {
	m := demoModel(t, "release")
	m = drive(t, m, keysOf("tab", "P")...)
	if m.focus != focusPick {
		t.Fatal("P did not open the picker")
	}
	f := tuitest.Frame(m)
	for _, want := range []string{"all projects", "kompass", "wetterwarte"} {
		if !strings.Contains(f, want) {
			t.Errorf("picker lacks %q:\n%s", want, f)
		}
	}
	m = drive(t, m, keysOf("esc")...)
	if m.focus != focusList || m.pin != nil {
		t.Errorf("esc: focus %v, pin %+v", m.focus, m.pin)
	}
	m = drive(t, m, keysOf("P", "j", "enter")...)
	if m.pin == nil || m.focus != focusList {
		t.Fatalf("enter: pin %+v, focus %v", m.pin, m.focus)
	}
	name := m.pin.name
	for _, h := range m.hits {
		if got := m.projectName(h); got != name {
			t.Errorf("hit of project %q under the pin %q", got, name)
		}
	}
	m = drive(t, m, keysOf("P", "k", "enter")...)
	if m.pin != nil {
		t.Errorf("all projects kept the pin %+v", m.pin)
	}

	// A click on the project label opens the picker, a click on an entry
	// applies it.
	left, right := m.columns()
	_, z := m.ruleRow(left, right)
	m = drive(t, m, click(z.project.Start, 2))
	if m.focus != focusPick {
		t.Fatal("the project label did not open the picker")
	}
	m = drive(t, m, click(5, chromeRows-1+1)) // second entry
	if m.pin == nil || m.focus == focusPick {
		t.Errorf("click on an entry: pin %+v, focus %v", m.pin, m.focus)
	}
}

// TestFiltersPerType: a filter a type does not apply is not sent to it, so
// the actor filter narrows the events but never makes the memory search an
// error.
func TestFiltersPerType(t *testing.T) {
	f := &fakeSearcher{}
	m := New(Options{Searcher: f, Now: tuitest.Clock, Debounce: time.Nanosecond, PreviewDebounce: time.Nanosecond})
	m = start(t, m, tea.WindowSizeMsg{Width: 100, Height: 20}, tuitest.Key("x"), tuitest.Key("tab"),
		tuitest.Key("s"), tuitest.Key("m"), tuitest.Key("a"), tuitest.Key("a"), tuitest.Key("ctrl+r"))
	byType := map[store.HitType]store.TypedQuery{}
	f.mu.Lock()
	for _, c := range f.calls {
		byType[c.q.Types[0]] = c.q
	}
	f.mu.Unlock()
	if len(byType) != 3 {
		t.Fatalf("searched types %v, want all three", byType)
	}
	if got := byType[store.TypeEvent]; got.Actor != "agent" || !got.Raw {
		t.Errorf("event query = actor %q raw %v, want agent and raw", got.Actor, got.Raw)
	}
	if got := byType[store.TypeMemory]; got.Actor != "" || !got.Raw {
		t.Errorf("memory query = actor %q raw %v, want no actor, raw", got.Actor, got.Raw)
	}
	if got := byType[store.TypeSession]; got.Actor != "" || got.Raw {
		t.Errorf("session query = actor %q raw %v, want neither", got.Actor, got.Raw)
	}
	if m.err != nil {
		t.Errorf("error: %v", m.err)
	}
}

// TestTruncatedGroups: a group cut off by the limit says so, in its
// header and in the status line.
func TestTruncatedGroups(t *testing.T) {
	m := New(Options{Now: tuitest.Clock})
	m.opts.Searcher = &fakeSearcher{}
	m.types = []store.HitType{store.TypeEvent, store.TypeMemory}
	m.query = "x"
	m.setGroups([]store.TypeHits{
		{Type: store.TypeEvent, Hits: make([]store.Hit, 2), Total: 1234},
		{Type: store.TypeMemory, Hits: make([]store.Hit, 1), Total: 1},
	})
	if got := m.status(); !strings.Contains(got, "event 2 of 1234, memory 1") {
		t.Errorf("status = %q", got)
	}
	if got := m.header(m.groups[0], 80); !strings.Contains(got, "2 of 1234, limit reached") {
		t.Errorf("header = %q", got)
	}
	m.types = m.types[:1]
	m.setGroups(m.groups[:1])
	if got := m.status(); !strings.Contains(got, "2 of 1234 hits, limit reached") {
		t.Errorf("single type status = %q", got)
	}
}

// TestOpenByType: enter opens the session of an event or session hit, and
// does nothing on a memory file, which has no detail view.
func TestOpenByType(t *testing.T) {
	m := demoModel(t, "release")
	m = drive(t, m, keysOf("tab", "s", "m")...)
	open := func(m Model) (OpenSession, bool) {
		cmd := m.open()
		if cmd == nil {
			return OpenSession{}, false
		}
		o, ok := cmd().(OpenSession)
		return o, ok
	}
	if o, ok := open(m); !ok || o.EventID == "" || o.SessionID == "" {
		t.Errorf("event hit: %+v, %v", o, ok)
	}
	m = moveTo(t, m, store.TypeSession)
	if o, ok := open(m); !ok || o.EventID != "" || o.SessionID != m.marked().Session.ID {
		t.Errorf("session hit: %+v, %v", o, ok)
	}
	m = moveTo(t, m, store.TypeMemory)
	if _, ok := open(m); ok {
		t.Error("a memory hit opened a session")
	}
}

// TestReloadKeepsMarkAcrossTypes: a reload keeps the mark on the same
// hit, also when it is a memory file, and shows no spinner.
func TestReloadKeepsMarkAcrossTypes(t *testing.T) {
	m := demoModel(t, demo.Query)
	m = drive(t, m, keysOf("tab", "m")...)
	m = moveTo(t, m, store.TypeMemory)
	key := hitKey(*m.marked())
	nm, cmd := m.Reload()
	if nm.(Model).searching {
		t.Error("reload shows the spinner")
	}
	m = tuitest.Run(t, nm, cmd).(Model)
	if got := hitKey(*m.marked()); got != key {
		t.Errorf("mark moved from %s to %s", key, got)
	}
}

// TestKindTypeKeys: the keys of the finer types switch them like e, s and
// m, and they search: input and answer split the events, image lists the
// newest events with an image without a word, file finds the backup copy
// of an edited file and shows its content in the preview.
func TestKindTypeKeys(t *testing.T) {
	m := demoModel(t, demo.Query)
	events := groupOf(m, store.TypeEvent).Total
	m = drive(t, m, keysOf("tab", "u")...)
	in := groupOf(m, store.TypeInput)
	if !m.hasType(store.TypeInput) || in == nil || in.Total == 0 || in.Total >= events {
		t.Fatalf("input: types %v, group %+v, events %d", m.types, in, events)
	}
	for _, h := range in.Hits {
		if h.Event == nil || h.Event.Event.Actor != model.ActorHuman || h.Type != store.TypeInput {
			t.Fatalf("hit %+v is no human input", h)
		}
	}
	m = drive(t, m, keysOf("e", "g")...)
	if a := groupOf(m, store.TypeAnswer); a == nil || a.Total == 0 || m.hasType(store.TypeEvent) {
		t.Errorf("answer: types %v, group %+v", m.types, a)
	}
	// The image type lists without a word.
	empty := demoModel(t, "")
	empty = drive(t, empty, keysOf("tab", "I", "e", "u")...)
	if g := groupOf(empty, store.TypeImage); g == nil || len(g.Hits) != 2 { // the image event and the text of its message
		t.Fatalf("image without a query: types %v, group %+v", empty.types, g)
	}
	// File history.
	files := demoModel(t, "smoothed")
	files = drive(t, files, keysOf("tab", "f", "e")...)
	g := groupOf(files, store.TypeFile)
	if g == nil || len(g.Hits) != 1 || g.Hits[0].File == nil || !strings.HasSuffix(g.Hits[0].File.Path, "route/elevation.go") {
		t.Fatalf("file group = %+v", g)
	}
	if f := tuitest.Frame(files); !strings.Contains(f, "route/elevation.go") || !strings.Contains(f, "smoothElevation") {
		t.Errorf("frame lacks the file and its content:\n%s", f)
	}
}

// TestStatusActors: the event group's status names the number of its
// matches per actor, so peer and automation events are visible as numbers
// and the sum can be checked (#383).
func TestStatusActors(t *testing.T) {
	m := demoModel(t, "route")
	g := m.groups[0]
	if g.Type != store.TypeEvent || len(g.Actors) == 0 {
		t.Fatalf("group = %+v", g)
	}
	sum := 0
	for _, a := range g.Actors {
		sum += a.N
	}
	if sum != g.Total {
		t.Errorf("actors sum to %d, Total = %d", sum, g.Total)
	}
	if st := m.status(); !strings.Contains(st, " [human ") || !strings.Contains(st, "agent ") {
		t.Errorf("status lacks the actors: %q", st)
	}
	// The actor cycle offers automation.
	if !slices.Contains(actors, "automation") {
		t.Errorf("actors = %v", actors)
	}
}
