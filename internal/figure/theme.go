package figure

import (
	"image/color"

	"github.com/seletz/total-recall/internal/canvas"
)

// Theme is the look of the figures: colours that read on a light and on a
// dark page. The zero value of every field falls back to its default, so a
// zero Theme is DefaultTheme with a transparent background.
type Theme struct {
	// Background is the colour behind the figure; the zero value (alpha 0)
	// is transparent, the page shows through.
	Background color.NRGBA
	// Text is the colour of titles, labels and values, a mid grey.
	Text color.NRGBA
	// Orange and Blue are stepped scales from light to dark, for the
	// heatmaps and for the segments of the working days.
	Orange, Blue []color.NRGBA
	// Palette is the categorical palette of the projects and categories,
	// in assignment order.
	Palette []color.NRGBA
}

// Defaults of the Theme, as #rrggbb; the configuration ([chart] text,
// scale_orange, scale_blue, palette) replaces them.
const (
	DefaultText = "#8b8b8b"
)

// DefaultOrange and DefaultBlue are the built-in stepped scales, four
// steps from light to dark.
var (
	DefaultOrange = []string{"#f6c9a8", "#ee9a5f", "#d9733a", "#a84f1c"}
	DefaultBlue   = []string{"#b3cde3", "#6a9fcf", "#3b73b0", "#1f4e85"}
)

// DefaultPalette is the built-in categorical palette. Its first six
// colours are the series colours of the TUI (project.Palette), so a
// project looks alike in the TUI and in the figures.
var DefaultPalette = []string{"#4e79a7", "#f28e2b", "#59a14f", "#e15759", "#b07aa1", "#76b7b2", "#edc948", "#9c755f", "#bab0ac"}

// white is the colour of the numbers on the dark steps of a heatmap.
var white = canvas.Hex("#ffffff")

// Fixed colours of the roles that are no configuration: the own messages
// (green), the weekend and the holidays of the working days.
const (
	humanHex   = "#59a14f"
	holidayHex = "#e15759"
)

// DefaultTheme returns the built-in theme: transparent background, grey
// text, the orange and blue scales and the categorical palette.
func DefaultTheme() Theme {
	return Theme{
		Text:    canvas.Hex(DefaultText),
		Orange:  mustColors(DefaultOrange),
		Blue:    mustColors(DefaultBlue),
		Palette: mustColors(DefaultPalette),
	}
}

func mustColors(hexes []string) []color.NRGBA {
	out := make([]color.NRGBA, len(hexes))
	for i, h := range hexes {
		out[i] = canvas.Hex(h)
	}
	return out
}

// resolved fills the unset parts of t with the defaults.
func (t Theme) resolved() Theme {
	d := DefaultTheme()
	if t.Text.A == 0 {
		t.Text = d.Text
	}
	if len(t.Orange) == 0 {
		t.Orange = d.Orange
	}
	if len(t.Blue) == 0 {
		t.Blue = d.Blue
	}
	if len(t.Palette) == 0 {
		t.Palette = d.Palette
	}
	return t
}

// emptyColor is the colour of an empty cell and of a grid line: the text-
// independent mid grey at a low opacity, which tints a light page darker
// and a dark page lighter.
var emptyColor = color.NRGBA{R: 127, G: 127, B: 127, A: 46}

// gridColor is the colour of grid lines.
var gridColor = color.NRGBA{R: 127, G: 127, B: 127, A: 76}

// human is the colour of the own messages.
func (t Theme) human() color.NRGBA { return canvas.Hex(humanHex) }

// agent is the colour of the agent activity.
func (t Theme) agent() color.NRGBA { return t.Orange[min(1, len(t.Orange)-1)] }

// step returns the i-th of a stepped scale, clamped to its ends.
func step(scale []color.NRGBA, i int) color.NRGBA {
	return scale[max(0, min(i, len(scale)-1))]
}
