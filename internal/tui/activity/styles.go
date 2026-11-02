package activity

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
)

// styles are the colours of the view for one background. The bars, the
// ratings and the weekend take their colours from chart.Palette, so the
// views of the TUI look alike; on a light background they are darkened
// for contrast. Everything coloured also differs in glyph or position, so
// the view stays readable without colours.
type styles struct {
	// Bar segments.
	core, outside, night, weekend lipgloss.Style
	// Axis background: outside the core hours, the core-hours band and the
	// lines at its start and end.
	empty, band, line lipgloss.Style
	// Span and difference by rating; weekendText for a weekend's row.
	under, on, over, weekendText lipgloss.Style
	// Chrome.
	faint, bold, label lipgloss.Style
}

// Palette colours by role (see chart.Palette).
var (
	colorBlue   = chart.Palette[0]
	colorOrange = chart.Palette[1]
	colorGreen  = chart.Palette[2]
	colorRed    = chart.Palette[3]
	colorPurple = chart.Palette[4]
	colorTeal   = chart.Palette[5]
)

// newStyles returns the styles for a dark or a light background.
func newStyles(dark bool) styles {
	// tone adapts a palette colour to the background: as is on dark,
	// darkened on light.
	tone := func(c color.Color) color.Color {
		if dark {
			return c
		}
		return lipgloss.Darken(c, 0.25)
	}
	ld := lipgloss.LightDark(dark)
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	bandBG := ld(lipgloss.Color("#e3ecf7"), lipgloss.Color("#1b2938"))
	bandFG := ld(lipgloss.Color("#9fb8d4"), lipgloss.Color("#3a5b82"))
	return styles{
		core:        fg(tone(colorBlue)),
		outside:     fg(tone(colorOrange)),
		night:       fg(tone(colorRed)),
		weekend:     fg(tone(colorPurple)),
		empty:       lipgloss.NewStyle().Faint(true),
		band:        fg(bandFG).Background(bandBG),
		line:        fg(tone(colorBlue)).Background(bandBG),
		under:       fg(tone(colorTeal)),
		on:          fg(tone(colorGreen)),
		over:        fg(tone(colorOrange)),
		weekendText: fg(tone(colorPurple)),
		faint:       lipgloss.NewStyle().Faint(true),
		bold:        lipgloss.NewStyle().Bold(true),
		label:       lipgloss.NewStyle().Bold(true).Foreground(tone(colorBlue)),
	}
}

// cell returns the style of a cell kind.
func (s styles) cell(k cellKind) lipgloss.Style {
	switch k {
	case cellBand:
		return s.band
	case cellLine:
		return s.line
	case cellCore:
		return s.core
	case cellOutside:
		return s.outside
	case cellNight:
		return s.night
	case cellWeekend:
		return s.weekend
	}
	return s.empty
}

// rating returns the style of a rating; RatingNone is unstyled.
func (s styles) rating(r store.Rating) lipgloss.Style {
	switch r {
	case store.RatingUnder:
		return s.under
	case store.RatingOn:
		return s.on
	case store.RatingOver:
		return s.over
	}
	return lipgloss.NewStyle()
}
