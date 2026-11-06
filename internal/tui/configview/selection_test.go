package configview

import (
	"image/color"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// nextBackground returns the background after the SGR parameters params
// of a sequence, given the background cur before it: a reset clears it,
// a 48;2 sets it, a 38 colour is skipped.
func nextBackground(cur, params string) string {
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "", "0", "49":
			cur = ""
		case "38":
			if i+1 < len(parts) && parts[i+1] == "2" {
				i += 4
			}
		case "48":
			if i+4 < len(parts) && parts[i+1] == "2" {
				cur = strings.Join(parts[i:i+5], ";")
				i += 4
			}
		}
	}
	return cur
}

// requireBackdrop checks that the background bg is drawn behind every
// visible cell of line from its first background on: a styled segment
// ends with a reset, so every segment after it must set the background
// again.
func requireBackdrop(t *testing.T, line, bg string) {
	t.Helper()
	cur, started := "", false
	rest := line
	for len(rest) > 0 {
		if strings.HasPrefix(rest, "\x1b[") {
			end := strings.IndexByte(rest, 'm')
			if end < 0 {
				t.Fatalf("unterminated sequence in %q", line)
			}
			cur = nextBackground(cur, rest[2:end])
			if cur == bg {
				started = true
			}
			rest = rest[end+1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(rest)
		if started && cur != bg {
			t.Errorf("%q is drawn without the background %s: %q", string(r), bg, line)
			return
		}
		rest = rest[size:]
	}
	if !started {
		t.Errorf("line has no background %s: %q", bg, line)
	}
}

// TestSelectedRowKeepsBackground checks the marked row of the settings,
// actions and setup pages on either background: the background runs over
// the whole row, and coloured text keeps a colour that reads on it.
func TestSelectedRowKeepsBackground(t *testing.T) {
	for _, tt := range []struct {
		name string
		bg   color.Color
		dark bool
	}{
		{"dark", color.Black, true},
		{"light", color.White, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			th := theme.New(tt.dark)
			bg := sgr(48, th.Selection)
			accent := sgr(38, th.OnSelection(th.Accent))
			warn := sgr(38, th.OnSelection(th.Warn))

			h := newTestEnv(t, "")
			h.writeSettings(t, "SessionStart", "Stop", "SubagentStop")
			h.found, h.exe, h.version = h.home+"/bin/total-recall", h.home+"/bin/total-recall", "1.2.0"
			start := func(keys ...string) Model {
				m := drive(t, New(h.options(t)), keys...)
				return tuitest.Send(t, m, tea.BackgroundColorMsg{Color: tt.bg}).(Model)
			}

			m := start("tab")
			line := rawLine(t, m, "▸")
			requireBackdrop(t, line, bg)
			requireStyled(t, line, "file", accent, bg)

			m = start("tab", "tab", "tab")
			line = rawLine(t, m, "▸")
			requireBackdrop(t, line, bg)

			m = start("shift+tab")
			requireBackdrop(t, rawLine(t, m, "▸"), bg)
			m = press(t, m, "j")
			line = rawLine(t, m, "▸")
			requireBackdrop(t, line, bg)
			requireStyled(t, rawLine(t, m, "lacks SessionEnd"), "×", warn)
		})
	}
}
