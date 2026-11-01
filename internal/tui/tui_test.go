package tui

import (
	"strings"
	"testing"

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
		{"Start", "", nil},
		{"StartStats", "stats", nil},
		{"Key2", "", []string{"2"}},
		{"Key1AfterStats", "stats", []string{"1"}},
		{"TabWraps", "", []string{"tab", "tab"}},
		{"HelpOpen", "", []string{"?"}},
		{"HelpOpenStats", "stats", []string{"?"}},
		{"HelpToggleOff", "", []string{"?", "?"}},
		{"HelpEscCloses", "", []string{"?", "esc"}},
		{"HelpSwallowsKeys", "", []string{"?", "2", "tab"}},
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
		{"q", []string{"q"}},
		{"ctrl+c", []string{"ctrl+c"}},
		{"ctrl+c in help", []string{"?", "ctrl+c"}},
		{"q in help", []string{"?", "q"}},
		{"q after help", []string{"?", "esc", "q"}},
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
