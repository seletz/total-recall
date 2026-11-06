// frame; and CollapseRepaints keeps only one paint. The terminal queries
// the program writes from its own goroutine (synchronized output, unicode
// core, kitty keyboard, and the background colour the views ask for) are
// removed as well (StripQueries).
// Package tuitest drives Bubble Tea models in tests, so every view package
// tests the same way. It wraps teatest with the fixed terminal size,
// colourless profile and clock the TUI golden tests share (New, Press,
// WaitFor, Final) and drives models without a program, running their
// commands synchronously (Exec, Run, Send, Drive).
package tuitest

import (
	"bytes"
	"io"
	"strings"
	"sync"
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

// Timeout is the ceiling for every wait on the program: it is the signal
// that ends a wait (an output, a handled message), never the clock, so a
// passing test is not slower for a high value. It is high because a
// parallel full test run can starve a program for many seconds.
const Timeout = 2 * time.Minute

// Gate lets a test wait for a condition on state that a probe model
// updates from the program's goroutine. The probe calls Notify after every
// change; Wait wakes on that signal instead of polling.
type Gate struct {
	mu sync.Mutex
	ch chan struct{}
}

// Notify wakes every Wait in progress to check its condition again.
func (g *Gate) Notify() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ch != nil {
		close(g.ch)
		g.ch = nil
	}
}

// Wait blocks until cond reports true, checking it again after every
// Notify, and fails the test after Timeout. cond runs without the Gate's
// lock; it takes the probe's own.
func (g *Gate) Wait(tb testing.TB, what string, cond func() bool) {
	tb.Helper()
	timeout := time.NewTimer(Timeout)
	defer timeout.Stop()
	for {
		g.mu.Lock()
		if g.ch == nil {
			g.ch = make(chan struct{})
		}
		ch := g.ch
		g.mu.Unlock()
		if cond() {
			return
		}
		select {
		case <-ch:
		case <-timeout.C:
			tb.Fatalf("timed out waiting for %s", what)
		}
	}
}

// Now is the fixed time Clock returns: a Tuesday noon in Berlin.
var Now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// Clock is the injected clock for models that take one.
func Clock() time.Time { return Now }

// Environ is the environment of every test program. The renderer picks its
// escape sequences from TERM, so it is fixed rather than inherited from the
// machine running the tests.
var Environ = []string{"TERM=xterm-256color"}

// New starts m in a test program of Width×Height cells without colours and
// with the fixed Environ, plus any further program options. The program is
// stopped when the test ends.
func New(tb testing.TB, m tea.Model, opts ...tea.ProgramOption) *teatest.TestModel {
	tb.Helper()
	opts = append([]tea.ProgramOption{tea.WithColorProfile(colorprofile.NoTTY), tea.WithEnvironment(Environ)}, opts...)
	tm := teatest.NewTestModel(tb, m, teatest.WithInitialTermSize(Width, Height), teatest.WithProgramOptions(opts...))
	tb.Cleanup(func() { tm.GetProgram().Kill() })
	return tm
}

// Key builds the key press for a name: a single character ("q", "?", "1"), a
// special key ("tab", "esc", "enter", "up", "down", "left", "right",
// "backspace", "space"), with the modifiers "ctrl+" and "shift+" in front
// ("ctrl+c", "shift+tab").
func Key(name string) tea.KeyPressMsg {
	var mod tea.KeyMod
	for {
		if rest, ok := strings.CutPrefix(name, "ctrl+"); ok {
			mod, name = mod|tea.ModCtrl, rest
		} else if rest, ok := strings.CutPrefix(name, "shift+"); ok {
			mod, name = mod|tea.ModShift, rest
		} else {
			break
		}
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

// WaitFor blocks until the program's output contains want, or fails the test
// after Timeout. Use it before sending keys that depend on an earlier,
// asynchronous step: the initial window size arrives from a goroutine, so a
// Resize right after New may be processed before it; and results of
// background commands show up only after they ran.
func WaitFor(tb testing.TB, tm *teatest.TestModel, want string) {
	tb.Helper()
	teatest.WaitFor(tb, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(want))
	}, teatest.WithDuration(Timeout), teatest.WithCheckInterval(10*time.Millisecond))
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

// SlowTicker is the program option for output goldens, see RequireOutput:
// it slows the renderer's ticker to one frame per second, so a start-up
// that quits within that second is painted once, by the final flush.
var SlowTicker = tea.WithFPS(1)

// RequireOutput quits the program and compares the raw renderer output with
// testdata/<test name>.golden. Update the files with go test -update.
//
// Where the renderer's first flush falls is a matter of scheduling: it runs
// on a wall-clock ticker, while the program hands over its start-up messages
// from goroutines. The program and teatest each send a window size message,
// and every resize forces a full repaint, so a start-up may paint its frame
// once, twice or three times, and a tick before the first view is rendered
// even starts it inline. Three things keep the golden independent of that:
// the program runs with SlowTicker, so a start-up that quits within a second
// is painted once, by the final flush; the model must already have its size
// when the program starts, so a tick that does come first paints the same
// frame; and CollapseRepaints keeps only one paint. The terminal queries
// the program writes from its own goroutine (synchronized output, unicode
// core, kitty keyboard, and the background colour the views ask for) are
// removed as well (StripQueries).
func RequireOutput(tb testing.TB, tm *teatest.TestModel) {
	tb.Helper()
	_ = tm.Quit()
	out, err := io.ReadAll(tm.FinalOutput(tb, teatest.WithFinalTimeout(Timeout)))
	if err != nil {
		tb.Fatal(err)
	}
	teatest.RequireEqualOutput(tb, CollapseRepaints(StripQueries(out)))
}

// StripQueries removes the start-up capability queries from raw program
// output; see RequireOutput.
func StripQueries(out []byte) []byte {
	for _, q := range []string{ansi.RequestModeSynchronizedOutput, ansi.RequestModeUnicodeCore, ansi.RequestKittyKeyboard, ansi.RequestBackgroundColor} {
		out = bytes.ReplaceAll(out, []byte(q), nil)
	}
	return out
}

// CollapseRepaints keeps only the last full repaint in raw program output:
// everything from the first erase of the screen up to the last one is
// dropped, so the result is the start-up sequences, one painted frame and
// the shutdown sequences. Output without a repeated erase is returned as is;
// see RequireOutput.
func CollapseRepaints(out []byte) []byte {
	erase := []byte(ansi.CursorHomePosition + ansi.EraseEntireScreen)
	first := bytes.Index(out, erase)
	last := bytes.LastIndex(out, erase)
	if first < 0 || first == last {
		return out
	}
	return append(out[:first:first], out[last:]...)
}
