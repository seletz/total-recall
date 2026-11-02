package tuitest_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/tui/tuitest"
)

func TestKey(t *testing.T) {
	tests := []struct {
		name string
		want tea.KeyPressMsg
	}{
		{"a", tea.KeyPressMsg{Code: 'a', Text: "a"}},
		{"?", tea.KeyPressMsg{Code: '?', Text: "?"}},
		{"ä", tea.KeyPressMsg{Code: 'ä', Text: "ä"}},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
		{"ctrl+space", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tuitest.Key(tt.name); got != tt.want {
				t.Errorf("Key(%q) = %+v, want %+v", tt.name, got, tt.want)
			}
		})
	}
	t.Run("unknown panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("Key(\"bogus\") did not panic")
			}
		}()
		tuitest.Key("bogus")
	})
}

type viewModel string

func (v viewModel) Init() tea.Cmd                       { return nil }
func (v viewModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return v, nil }
func (v viewModel) View() tea.View                      { return tea.NewView(string(v)) }

func TestFrame(t *testing.T) {
	tests := []struct {
		name string
		view string
		want string
	}{
		{"empty", "", "\n"},
		{"trailing spaces trimmed", "a   \nb  ", "a\nb\n"},
		{"styles stripped", "\x1b[1mbold\x1b[0m   \nplain", "bold\nplain\n"},
		{"blank rows kept", "a\n\n   \nb", "a\n\n\nb\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tuitest.Frame(viewModel(tt.view)); got != tt.want {
				t.Errorf("Frame = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripQueries(t *testing.T) {
	in := ansi.RequestModeSynchronizedOutput + "frame" + ansi.RequestKittyKeyboard + ansi.RequestModeUnicodeCore + "\x1b[1mx"
	if got := string(tuitest.StripQueries([]byte(in))); got != "frame\x1b[1mx" {
		t.Errorf("StripQueries = %q", got)
	}
	if got := tuitest.StripQueries(nil); len(got) != 0 {
		t.Errorf("StripQueries(nil) = %q", got)
	}
	if strings.Contains(string(tuitest.StripQueries([]byte(ansi.RequestKittyKeyboard))), "\x1b") {
		t.Error("query left in output")
	}
}

func TestCollapseRepaints(t *testing.T) {
	erase := ansi.CursorHomePosition + ansi.EraseEntireScreen
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"nil", "", ""},
		{"no paint", "setup\x1b[?25h", "setup\x1b[?25h"},
		{"one paint", "setup" + erase + "frame\x1b[?25h", "setup" + erase + "frame\x1b[?25h"},
		{"repainted", "setup" + erase + "frame\x1b[?25h\x1b[?25l" + erase + "frame\x1b[?25h", "setup" + erase + "frame\x1b[?25h"},
		{"empty first paint", "setup" + erase + erase + "frame", "setup" + erase + "frame"},
		{"last paint kept", "setup" + erase + "a" + erase + "b" + erase + "c", "setup" + erase + "c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tuitest.CollapseRepaints([]byte(tt.in))); got != tt.want {
				t.Errorf("CollapseRepaints(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
