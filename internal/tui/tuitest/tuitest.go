// Package tuitest drives Bubble Tea models in tests. It wraps teatest with
// the fixed terminal size, colourless profile and clock the TUI golden tests
// share, so every view package tests the same way.
package tuitest

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// Width and Height are the terminal size of every test program.
const (
	Width  = 100
	Height = 30
)

// Timeout is how long Final and WaitQuit wait for the program to stop.
const Timeout = 5 * time.Second

// Now is the fixed time Clock returns: a Tuesday noon in Berlin.
var Now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// Clock is the injected clock for models that take one.
func Clock() time.Time { return Now }

// Environ is the environment of every test program. The renderer picks its
// escape sequences from TERM, so it is fixed rather than inherited from the
// machine running the tests.
var Environ = []string{"TERM=xterm-256color"}

// New starts m in a test program of Width×Height cells without colours and
// with the fixed Environ. The program is stopped when the test ends.
func New(tb testing.TB, m tea.Model, opts ...teatest.TestOption) *teatest.TestModel {
	tb.Helper()
	opts = append([]teatest.TestOption{
		teatest.WithInitialTermSize(Width, Height),
		teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.NoTTY), tea.WithEnvironment(Environ)),
	}, opts...)
	tm := teatest.NewTestModel(tb, m, opts...)
	tb.Cleanup(func() { tm.GetProgram().Kill() })
	return tm
}

// Key builds the key press for a name: a single character ("q", "?", "1"), a
// special key ("tab", "esc", "enter", "up", "down", "left", "right",
// "backspace", "space") or a ctrl combination ("ctrl+c").
func Key(name string) tea.KeyPressMsg {
	var mod tea.KeyMod
	if rest, ok := strings.CutPrefix(name, "ctrl+"); ok {
		mod, name = tea.ModCtrl, rest
	}
	special := map[string]rune{
		"tab": tea.KeyTab, "esc": tea.KeyEscape, "enter": tea.KeyEnter,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"backspace": tea.KeyBackspace, "space": tea.KeySpace,
	}
	if code, ok := special[name]; ok {
		k := tea.KeyPressMsg{Code: code, Mod: mod}
		if code == tea.KeySpace && mod == 0 {
			k.Text = " "
		}
		return k
	}
	r := []rune(name)
	if len(r) != 1 {
		panic("tuitest.Key: unknown key " + name)
	}
	k := tea.KeyPressMsg{Code: r[0], Mod: mod}
	if mod == 0 {
		k.Text = name
	}
	return k
}

// Press sends the named keys (see Key) in order.
func Press(tm *teatest.TestModel, names ...string) {
	for _, n := range names {
		tm.Send(Key(n))
	}
}

// Resize sends a new terminal size.
func Resize(tm *teatest.TestModel, width, height int) {
	tm.Send(tea.WindowSizeMsg{Width: width, Height: height})
}

// Final quits the program after all messages sent so far have been handled
// and returns the final model.
func Final(tb testing.TB, tm *teatest.TestModel) tea.Model {
	tb.Helper()
	_ = tm.Quit()
	return tm.FinalModel(tb, teatest.WithFinalTimeout(Timeout))
}

// WaitQuit fails the test if the program does not stop by itself within
// Timeout.
func WaitQuit(tb testing.TB, tm *teatest.TestModel) {
	tb.Helper()
	tm.WaitFinished(tb, teatest.WithFinalTimeout(Timeout), teatest.WithTimeoutFn(func(tb testing.TB) {
		tb.Fatal("program did not quit")
	}))
}

// Frame renders the model's view as plain text: escape sequences removed,
// trailing spaces trimmed, one line per terminal row.
func Frame(m tea.Model) string {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// RequireOutput quits the program and compares the raw renderer output with
// testdata/<test name>.golden. Update the files with go test -update.
func RequireOutput(tb testing.TB, tm *teatest.TestModel) {
	tb.Helper()
	_ = tm.Quit()
	out, err := io.ReadAll(tm.FinalOutput(tb, teatest.WithFinalTimeout(Timeout)))
	if err != nil {
		tb.Fatal(err)
	}
	teatest.RequireEqualOutput(tb, out)
}
