package theme

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/tui/chart"
)

func rgb(c color.Color) [3]uint32 {
	r, g, b, _ := c.RGBA()
	return [3]uint32{r >> 8, g >> 8, b >> 8}
}

// TestPalette checks the palette selection: on a dark background the hues
// are the series colours of the charts as they are, on a light background
// they are darkened for contrast, and the roles keep their hue either way.
func TestPalette(t *testing.T) {
	dark, light := New(true), New(false)
	if !dark.Dark || light.Dark {
		t.Fatalf("Dark flags: dark %v, light %v", dark.Dark, light.Dark)
	}
	tests := []struct {
		name        string
		dark, light color.Color
		base        color.Color
	}{
		{"accent", dark.Accent, light.Accent, chart.Palette[0]},
		{"human", dark.Human, light.Human, chart.Palette[1]},
		{"agent", dark.Agent, light.Agent, chart.Palette[0]},
		{"peer", dark.Peer, light.Peer, chart.Palette[4]},
		{"tool", dark.Tool, light.Tool, chart.Palette[5]},
		{"warn", dark.Warn, light.Warn, chart.Palette[3]},
		{"holiday", dark.Holiday, light.Holiday, chart.HolidayColor},
		{"weekend", dark.Weekend, light.Weekend, chart.Palette[4]},
		{"under", dark.Under, light.Under, chart.Palette[5]},
		{"on", dark.On, light.On, chart.Palette[2]},
		{"over", dark.Over, light.Over, chart.Palette[1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rgb(tt.dark) != rgb(tt.base) {
				t.Errorf("dark = %v, want the palette colour %v", rgb(tt.dark), rgb(tt.base))
			}
			d, l := rgb(tt.dark), rgb(tt.light)
			for i := range d {
				if l[i] > d[i] {
					t.Errorf("light = %v is not darker than dark = %v", l, d)
					break
				}
			}
			if l == d {
				t.Errorf("light = %v equals dark; it should be darkened", l)
			}
		})
	}
}

// TestTone checks that Tone keeps a colour on a dark background and darkens
// it on a light one.
func TestTone(t *testing.T) {
	c := lipgloss.Color("#f28e2b")
	if got := New(true).Tone(c); rgb(got) != rgb(c) {
		t.Errorf("dark Tone = %v, want %v", rgb(got), rgb(c))
	}
	if got := New(false).Tone(c); rgb(got)[0] >= rgb(c)[0] {
		t.Errorf("light Tone = %v is not darker than %v", rgb(got), rgb(c))
	}
}

// TestGradient checks that the gradient keeps the text, colours every rune
// with a different colour from the first to the last, and renders a short
// text in the title style.
func TestGradient(t *testing.T) {
	th := New(true)
	tests := []struct {
		in   string
		want int // distinct colours at least
	}{
		{"", 0},
		{"t", 1},
		{"total-recall", 4},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := th.Gradient(tt.in)
			if plain := ansi.Strip(got); plain != tt.in {
				t.Fatalf("Gradient(%q) strips to %q", tt.in, plain)
			}
			colours := map[string]bool{}
			for _, part := range strings.Split(got, "\x1b[") {
				if i := strings.Index(part, "38;2;"); i >= 0 {
					colours[part[i:strings.Index(part, "m")]] = true
				}
			}
			if len(colours) < tt.want {
				t.Errorf("Gradient(%q) uses %d colours, want at least %d:\n%q", tt.in, len(colours), tt.want, got)
			}
		})
	}
	if got := New(false).Gradient("ab"); ansi.Strip(got) != "ab" {
		t.Errorf("light Gradient = %q", got)
	}
}

// TestActor checks that every actor gets its own colour and an unknown
// actor stays plain.
func TestActor(t *testing.T) {
	th := New(true)
	seen := map[[3]uint32]model.Actor{}
	for _, a := range []model.Actor{model.ActorHuman, model.ActorAgent, model.ActorPeer, model.ActorTool, model.ActorAutomation} {
		fg := th.Actor(a).GetForeground()
		if _, none := fg.(lipgloss.NoColor); none {
			t.Errorf("Actor(%q) has no colour", a)
			continue
		}
		if prev, ok := seen[rgb(fg)]; ok && prev != model.ActorTool && a != model.ActorTool {
			t.Errorf("Actor(%q) shares its colour with %q", a, prev)
		}
		seen[rgb(fg)] = a
	}
	if fg, none := th.Actor("bogus").GetForeground().(lipgloss.NoColor); !none {
		t.Errorf("Actor(bogus) = %v, want plain", fg)
	}
}

// TestStylesReadWithoutColour checks that the styles that mark something
// (matches, the selected match, the active tab) also set an attribute, so
// the mark survives NO_COLOR.
func TestStylesReadWithoutColour(t *testing.T) {
	th := New(true)
	tests := []struct {
		name string
		ok   bool
	}{
		{"match", th.Match.GetBold() && th.Match.GetUnderline()},
		{"selected", th.Selected.GetBold()},
		{"selected match", th.SelectedMatch.GetReverse() && th.SelectedMatch.GetBold()},
		{"active", th.Active.GetBold()},
		{"title", th.Title.GetBold()},
		{"muted", th.Muted.GetFaint()},
		{"mark", th.Mark.GetBold()},
	}
	for _, tt := range tests {
		if !tt.ok {
			t.Errorf("style %s lacks its attribute", tt.name)
		}
	}
}

// TestOnSelection checks that every project colour reads on the selection
// background of either theme, that a colour that reads already stays, and
// that a colour too close to the background (configured freely) is moved
// away from it.
func TestOnSelection(t *testing.T) {
	colours := append([]color.Color{lipgloss.Color("#101010"), lipgloss.Color("#f0f0f0"), lipgloss.Color("#243447")}, chart.Palette...)
	for _, dark := range []bool{true, false} {
		th := New(dark)
		for _, c := range colours {
			got := th.OnSelection(th.Tone(c))
			if r := contrast(got, th.Selection); r < minContrast {
				t.Errorf("dark=%v: %v on the selection has contrast %.2f", dark, c, r)
			}
		}
	}
	// Teal on the dark selection: 5.5:1 needs no change.
	th := New(true)
	if got := th.OnSelection(chart.Palette[5]); rgb(got) != rgb(chart.Palette[5]) {
		t.Errorf("OnSelection(teal) = %v, want it unchanged", got)
	}
}

// TestHelp checks that the help styles are coloured and the key is bold.
func TestHelp(t *testing.T) {
	h := New(true).Help()
	coloured := func(s lipgloss.Style) bool { _, none := s.GetForeground().(lipgloss.NoColor); return !none }
	if !h.ShortKey.GetBold() || !coloured(h.ShortKey) || !coloured(h.ShortDesc) {
		t.Errorf("help styles: key %v, desc %v", h.ShortKey, h.ShortDesc)
	}
	if !coloured(h.FullKey) || !coloured(h.FullDesc) {
		t.Errorf("full help styles: key %v, desc %v", h.FullKey, h.FullDesc)
	}
}
