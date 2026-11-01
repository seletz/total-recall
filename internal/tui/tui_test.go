package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/tui/tuitest"
)

func TestParseGraphics(t *testing.T) {
	tests := []struct {
		in      string
		want    Graphics
		wantErr bool
	}{
		{"", GraphicsAuto, false},
		{"auto", GraphicsAuto, false},
		{"text", GraphicsText, false},
		{"kitty", GraphicsKitty, false},
		{"Text", "", true},
		{"sixel", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseGraphics(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseGraphics(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseGraphics(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewRejectsUnknownView(t *testing.T) {
	if _, err := New(Options{Start: "bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("New(Start: bogus) error = %v", err)
	}
}

// TestFrames drives the app with key presses and compares the final frame
// (the rendered View, without escape sequences) against a golden file.
func TestFrames(t *testing.T) {
	tests := []struct {
		name  string
		start string
		keys  []string
	}{
		// The search view starts with its input focused: the first tab moves
		// the focus to the hit list, only then do the global keys apply.
		{"Start", "", nil},
		{"StartStats", "stats", nil},
		{"TypedIntoSearch", "", []string{"2", "q", "?"}},
		{"Key2", "", []string{"tab", "2"}},
		{"Key1AfterStats", "stats", []string{"1"}},
		{"TabWraps", "", []string{"tab", "tab", "tab"}},
		{"HelpOpen", "", []string{"tab", "?"}},
		{"HelpOpenStats", "stats", []string{"?"}},
		{"HelpToggleOff", "", []string{"tab", "?", "?"}},
		{"HelpEscCloses", "", []string{"tab", "?", "esc"}},
		{"HelpSwallowsKeys", "", []string{"tab", "?", "2", "tab"}},
		{"Resize", "", []string{"resize"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Start: tt.start, Now: tuitest.Clock})
			if err != nil {
				t.Fatal(err)
			}
			tm := tuitest.New(t, app)
			for _, k := range tt.keys {
				if k == "resize" {
					// Wait for the first frame so the initial size has been
					// processed before the new one arrives.
					tuitest.WaitFor(t, tm, "total-recall")
					tuitest.Resize(tm, 60, 12)
					continue
				}
				tuitest.Press(tm, k)
			}
			final := tuitest.Final(t, tm).(App)
			golden.RequireEqual(t, tuitest.Frame(final))
		})
	}
}

// TestOutput is the teatest basis: the raw renderer output of a start-up.
func TestOutput(t *testing.T) {
	app, err := New(Options{Now: tuitest.Clock})
	if err != nil {
		t.Fatal(err)
	}
	tm := tuitest.New(t, app)
	tuitest.RequireOutput(t, tm)
}

func TestQuit(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"q from list", []string{"tab", "q"}},
		{"q from stats", []string{"tab", "2", "q"}},
		{"ctrl+c", []string{"ctrl+c"}},
		{"ctrl+c in search input", []string{"x", "ctrl+c"}},
		{"ctrl+c in help", []string{"tab", "?", "ctrl+c"}},
		{"q in help", []string{"tab", "?", "q"}},
		{"q after help", []string{"tab", "?", "esc", "q"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Now: tuitest.Clock})
			if err != nil {
				t.Fatal(err)
			}
			tm := tuitest.New(t, app)
			tuitest.Press(tm, tt.keys...)
			// Fails with a timeout if the program does not quit by itself.
			tuitest.WaitQuit(t, tm)
		})
	}
}

// TestSearchWiring checks that New hands the initial query to the search
// view: it shows up in the input, and without a store the view says so.
func TestSearchWiring(t *testing.T) {
	app, err := New(Options{Query: "release", Now: tuitest.Clock})
	if err != nil {
		t.Fatal(err)
	}
	tm := tuitest.New(t, app)
	final := tuitest.Final(t, tm).(App)
	frame := tuitest.Frame(final)
	for _, want := range []string{"> release", "no database"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q:\n%s", want, frame)
		}
	}
}

// TestCursorBelowHeader checks that the app shifts the search input's cursor
// past the header and the rule, and drops it while the help is open.
func TestCursorBelowHeader(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want bool
	}{
		{"input focused", nil, true},
		{"help open", []string{"tab", "?"}, false},
		{"stats", []string{"tab", "2"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Query: "ab", Now: tuitest.Clock})
			if err != nil {
				t.Fatal(err)
			}
			var m tea.Model = app
			m, _ = m.Update(tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height})
			for _, k := range tt.keys {
				m, _ = m.Update(tuitest.Key(k))
			}
			c := m.View().Cursor
			if (c != nil) != tt.want {
				t.Fatalf("cursor present = %v, want %v", c != nil, tt.want)
			}
			if c != nil && (c.X != 5 || c.Y != 2) {
				t.Errorf("cursor = (%d,%d), want (5,2)", c.X, c.Y)
			}
		})
	}
}
