package configview

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// twoProjects are the definitions of the projects tests: the alpha
// repository of the Small fixture by its remote URL and the gamma
// directory by its path; beta stays unmatched.
const twoProjects = `
[[project]]
name = "kompass"
description = "Wanderkarten"
color = "blue"
match = ["https://example.test/o/alpha", "~/src/kompass-*"]

[[project]]
name = "notizen"
match = "/w/gamma"
`

// typeText presses the characters of s.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = press(t, m, string(r))
	}
	return m
}

// projects opens the config view on the projects page.
func projects(t *testing.T, h *testEnv) Model {
	t.Helper()
	return drive(t, New(h.options(t)), "tab", "tab")
}

func requireFile(t *testing.T, h *testEnv, want ...string) {
	t.Helper()
	b, err := os.ReadFile(h.cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		if !strings.Contains(string(b), w) {
			t.Errorf("file lacks %q:\n%s", w, b)
		}
	}
}

func requireNotInFile(t *testing.T, h *testEnv, text string) {
	t.Helper()
	b, _ := os.ReadFile(h.cfg.Path)
	if strings.Contains(string(b), text) {
		t.Errorf("file still has %q:\n%s", text, b)
	}
}

// TestProjectsList shows the definitions with their sessions, the paths of
// the archive without a definition, and the details of the marked row.
func TestProjectsList(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	f := frame(m)
	requireContains(t, f, "[projects]", "NAME", "DESCRIPTION", "SESSIONS", "MATCH",
		"▸ ■ kompass  Wanderkarten", "9  https://example.test/o/alpha, ~/src/kompass-*",
		"· notizen", "3  /w/gamma",
		"UNMATCHED", "LAST", "PATH", "beta", "4  ", "/w/beta  https://example.test/o/beta",
		"kompass  #4e79a7  matches 1 path · 9 sessions: alpha",
		"enter edits · a adds · x removes · K/J move (first match wins) · m adds a path as a pattern")
	m = press(t, m, "j")
	requireContains(t, frame(m), "▸ · notizen", "notizen  automatic colour  matches 1 path · 3 sessions: /w/gamma")
	m = press(t, m, "j")
	requireContains(t, frame(m), "▸ beta", "beta  4 sessions, last", "a makes it a project, m adds it to one")
	// The settings page shows the table as one read-only row.
	m = press(t, m, "shift+tab")
	m = goTo(t, m, "project")
	requireContains(t, frame(m), "[project]", "kompass (#4e79a7)")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "project is a table: edit it in the file")
}

// TestProjectsFromUnmatched makes a path of the archive a definition: the
// form opens with the derived name and the remote URL, the preview names
// what it matches, and confirm writes it.
func TestProjectsFromUnmatched(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	m = press(t, m, "j", "j", "enter")
	f := frame(m)
	requireContains(t, f, "new project", "name          beta", "description   none", "color         · automatic",
		"match         https://example.test/o/beta", "+ pattern",
		"preview  matches 1 path · 4 sessions: beta", "y saves", "unsaved changes")
	if !m.Focused() {
		t.Error("the form does not take the keys")
	}
	m = press(t, m, "y")
	f = frame(m)
	requireContains(t, f, "beta written to ~/.config/total-recall/config.toml · stats and activity reload", "▸ · beta")
	if strings.Contains(f, "UNMATCHED") {
		t.Errorf("beta still unmatched:\n%s", f)
	}
	requireFile(t, h, "\n[[project]]\nname = \"beta\"\nmatch = \"https://example.test/o/beta\"\n")
	if m.opts.Config.Projects[2].Name != "beta" {
		t.Errorf("view config = %+v", m.opts.Config.Projects)
	}
}

// TestProjectsForm edits every field of a definition: description and
// name by input, the colour in the picker, a pattern added with its
// preview, a pattern removed; confirm writes it all at once.
func TestProjectsForm(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	m = press(t, m, "enter")
	requireContains(t, frame(m), "project kompass  1 of 2 · first match wins", "▸ name          kompass", "color         ■ blue #4e79a7",
		"preview  matches 1 path · 9 sessions: alpha", "enter edits the marked field · a adds a pattern · x removes it · y saves · esc leaves")
	// Description.
	m = press(t, m, "j", "enter")
	requireContains(t, frame(m), "> Wanderkarten", "enter takes it, esc cancels")
	m = press(t, m, "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	m = typeText(t, m, "karten")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "description   Wanderkarten", "unsaved changes")
	// Colour: the picker, two to the right.
	m = press(t, m, "j", "enter")
	requireContains(t, frame(m), "color: · automatic  ■ [blue] ■ orange  ■ green", "custom #rrggbb")
	m = press(t, m, "right", "enter")
	requireContains(t, frame(m), "color         ■ orange #f28e2b")
	// A pattern, with the preview while typing.
	m = press(t, m, "a")
	requireContains(t, frame(m), "* matches any text", "preview  matches 1 path · 9 sessions: alpha")
	m = typeText(t, m, "/w/be*")
	requireContains(t, frame(m), "pattern /w/be*  matches 1 path · 4 sessions: beta")
	m = typeText(t, m, "x")
	requireContains(t, frame(m), "pattern /w/be*x  matches nothing in the archive")
	m = press(t, m, "backspace", "enter")
	requireContains(t, frame(m), "match         /w/be*", "pattern /w/be*  matches 1 path · 4 sessions: beta")
	// Remove the second pattern of the file.
	m = press(t, m, "k")
	requireContains(t, frame(m), "▸ match         ~/src/kompass-*")
	m = press(t, m, "x")
	if f := frame(m); strings.Contains(f, "kompass-*") {
		t.Errorf("pattern not removed:\n%s", f)
	}
	m = press(t, m, "y")
	requireContains(t, frame(m), "kompass written to", "■ kompass  Wanderkarten", "13  https://example.test/o/alpha, /w/be*")
	requireFile(t, h, "name = \"kompass\"\ndescription = \"Wanderkarten\"\ncolor = \"orange\"\nmatch = [\"https://example.test/o/alpha\", \"/w/be*\"]\n")
	if f := frame(m); strings.Contains(f, "UNMATCHED") {
		t.Errorf("beta still unmatched:\n%s", f)
	}
}

// TestProjectsColorCustom takes a #rrggbb value in the picker and rejects
// a wrong one; d sets the colour back to automatic.
func TestProjectsColorCustom(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	m = press(t, m, "enter", "j", "j", "enter", "left", "left", "enter")
	requireContains(t, frame(m), "> #", "a palette name or #rrggbb")
	m = typeText(t, m, "12345")
	m = press(t, m, "enter")
	requireContains(t, frame(m), `color "#12345": use blue, orange`)
	m = typeText(t, m, "6")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "color         ■ #123456")
	m = press(t, m, "d")
	requireContains(t, frame(m), "color         · automatic")
	press(t, m, "y")
	requireFile(t, h, "name = \"kompass\"\ndescription = \"Wanderkarten\"\n# color = \"blue\"\n")
}

// TestProjectsRejects: a draft without a name or pattern is not written,
// the file stays, and esc asks before unsaved changes are dropped.
func TestProjectsRejects(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	before, _ := os.ReadFile(h.cfg.Path)
	m := projects(t, h)
	m = press(t, m, "a")
	requireContains(t, frame(m), "new project", "preview: a name is required", "> ")
	m = typeText(t, m, "iris")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "preview: a pattern is required, e.g. the remote URL or ~/src/name")
	m = press(t, m, "y")
	requireContains(t, frame(m), `project "iris": no match patterns`)
	m = press(t, m, "a", "esc")
	requireContains(t, frame(m), "▸"+strings.Repeat(" ", 15)+"+ pattern")
	m = press(t, m, "k", "k", "k", "enter", "backspace", "backspace", "backspace", "backspace")
	m = typeText(t, m, "kompass")
	m = press(t, m, "enter", "a")
	m = typeText(t, m, "*")
	requireContains(t, frame(m), `preview: project "kompass" is listed twice`)
	m = press(t, m, "enter", "y")
	requireContains(t, frame(m), `project "kompass" is listed twice`)
	after, _ := os.ReadFile(h.cfg.Path)
	if string(after) != string(before) {
		t.Errorf("file changed:\n%s", after)
	}
	m = press(t, m, "esc")
	requireContains(t, frame(m), "unsaved changes: y saves, esc again leaves")
	m = press(t, m, "esc")
	requireContains(t, frame(m), "▸ ■ kompass")
	if m.Focused() {
		t.Error("the list takes the keys")
	}
}

// TestProjectsRemoveMove removes a definition after a confirmation and
// moves one in the file.
func TestProjectsRemoveMove(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	m = press(t, m, "J")
	requireContains(t, frame(m), "kompass moved down · first match wins", "▸ ■ kompass")
	requireFile(t, h, "name = \"notizen\"\nmatch = \"/w/gamma\"\n\n[[project]]\nname = \"kompass\"")
	m = press(t, m, "K")
	requireContains(t, frame(m), "kompass moved up")
	requireFile(t, h, "name = \"kompass\"\ndescription = \"Wanderkarten\"\ncolor = \"blue\"\nmatch = [\"https://example.test/o/alpha\", \"~/src/kompass-*\"]\n\n[[project]]\nname = \"notizen\"")
	m = press(t, m, "j", "x")
	requireContains(t, frame(m), "remove notizen from ~/.config/total-recall/config.toml? y confirms, esc cancels")
	m = press(t, m, "esc")
	requireContains(t, frame(m), "· notizen")
	m = press(t, m, "x", "y")
	f := frame(m)
	requireContains(t, f, "notizen removed from ~/.config/total-recall/config.toml", "UNMATCHED", "/w/gamma")
	if strings.Contains(f, "· notizen") {
		t.Errorf("notizen still listed:\n%s", f)
	}
	requireNotInFile(t, h, "notizen")
}

// TestProjectsPattern adds a path of the archive as a pattern of a chosen
// definition.
func TestProjectsPattern(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	m = press(t, m, "m")
	requireContains(t, frame(m), "mark a path under UNMATCHED")
	m = press(t, m, "j", "j", "m")
	requireContains(t, frame(m), `add "https://example.test/o/beta" to the marked project? enter confirms, esc cancels`, "▸ ■ kompass")
	m = press(t, m, "j", "enter")
	requireContains(t, frame(m), `"https://example.test/o/beta" added to notizen`, "7  /w/gamma, https://example.test/o/beta")
	requireFile(t, h, "name = \"notizen\"\nmatch = [\"/w/gamma\", \"https://example.test/o/beta\"]\n")
}

// TestProjectsShadowed shows which earlier definition takes a path that a
// later one matches too, in the list and in the form.
func TestProjectsShadowed(t *testing.T) {
	h := newActivityEnv(t, `
[[project]]
name = "alles"
match = "/w/*"

[[project]]
name = "kompass"
match = "https://example.test/o/alpha"
`)
	m := projects(t, h)
	m = press(t, m, "j")
	requireContains(t, frame(m), "kompass  automatic colour  alpha goes to alles (earlier)")
	m = press(t, m, "enter")
	requireContains(t, frame(m), "preview  alpha goes to alles (earlier)")
	m = press(t, m, "esc", "K")
	requireContains(t, frame(m), "kompass  automatic colour  matches 1 path · 9 sessions: alpha")
}

// TestProjectsProblem: a broken list is shown with its problem, and the
// entries stay editable.
func TestProjectsProblem(t *testing.T) {
	h := newActivityEnv(t, "\n[[project]]\nname = \"x\"\ncolor = \"mauve\"\nmatch = \"/w/alpha\"\n")
	m := projects(t, h)
	requireContains(t, frame(m), "[[project]] has a problem", "mauve", "· x", "mauve?")
	m = press(t, m, "enter", "j", "j", "d", "y")
	requireContains(t, frame(m), "x written to", "x  automatic colour  matches 1 path · 9 sessions: alpha")
	requireFile(t, h, "# color = \"mauve\"")
}

// TestProjectsEmpty: without definitions the page says how to start.
func TestProjectsEmpty(t *testing.T) {
	h := newActivityEnv(t, "")
	m := projects(t, h)
	requireContains(t, frame(m), "no project definitions yet", "UNMATCHED", "alpha", "beta", "/w/gamma",
		"alpha  9 sessions, last")
	m = press(t, m, "a")
	requireContains(t, frame(m), "new project", "name          alpha", "match         https://example.test/o/alpha", "preview  matches 1 path · 9 sessions: alpha")
}

// TestProjectsMouse: a click marks a row, a second click opens it; a click
// on a colour of the picker picks it.
func TestProjectsMouse(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	click := func(x, y int) {
		m = tuitest.Send(t, m, tea.MouseClickMsg{X: x, Y: y + chromeRows, Button: tea.MouseLeft}).(Model)
	}
	click(5, 2)
	requireContains(t, frame(m), "▸ · notizen")
	click(5, 2)
	requireContains(t, frame(m), "project notizen  2 of 2")
	click(5, 3) // the colour field
	click(5, 3)
	requireContains(t, frame(m), "color: · [automatic] ■ blue")
	page := m.pages[pageProjects].(projectsPage)
	_, spans := page.picker(&m)
	click(spans[3].Start+1, page.pickerRow(&m))
	requireContains(t, frame(m), "color         ■ green #59a14f")
	// The wheel moves the mark of the list.
	m = press(t, m, "esc", "esc")
	m = tuitest.Send(t, m, tea.MouseWheelMsg{Y: 5, Button: tea.MouseWheelUp}).(Model)
	requireContains(t, frame(m), "▸ ■ kompass")
}

// TestProjectsChanged: a definition written elsewhere (the editor) shows
// after Changed, and a rename keeps the mark on the definition.
func TestProjectsChanged(t *testing.T) {
	h := newActivityEnv(t, twoProjects)
	m := projects(t, h)
	m = press(t, m, "j")
	cfg, err := m.opts.Config.SetProject(1, config.Project{Name: "notizbox", Match: config.StringList{"/w/gamma"}})
	if err != nil {
		t.Fatal(err)
	}
	m = tuitest.Send(t, m, Changed{Config: cfg}).(Model)
	requireContains(t, frame(m), "▸ · notizbox")
}
