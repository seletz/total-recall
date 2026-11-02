package activity

import (
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// styles are the colours of the view for one background, derived from the
// theme: the bar segments take the series colours of the charts by role
// (core hours blue, outside orange, night red, weekend purple, holiday
// magenta), the ratings the theme's under/on/over colours. The band of the
// core hours is the view's own: a tinted background with a dim line.
// Everything coloured also differs in glyph or position, so the view stays
// readable without colours.
type styles struct {
	// Bar segments.
	core, outside, night, weekend, holiday lipgloss.Style
	// Axis background: outside the core hours, the core-hours band and the
	// lines at its start and end.
	empty, band, line lipgloss.Style
	// Span and difference by rating; weekendText for a weekend's row,
	// holidayText for a holiday's row and the holiday names, holidayName
	// for the name on the axis.
	under, on, over, weekendText, holidayText, holidayName lipgloss.Style
	// Chrome.
	faint, bold, label, active lipgloss.Style
}

// newStyles returns the styles for th.
func newStyles(th theme.Theme) styles {
	ld := lipgloss.LightDark(th.Dark)
	bandBG := ld(lipgloss.Color("#e3ecf7"), lipgloss.Color("#1b2938"))
	bandFG := ld(lipgloss.Color("#9fb8d4"), lipgloss.Color("#3a5b82"))
	return styles{
		core:        lipgloss.NewStyle().Foreground(th.Accent),
		outside:     lipgloss.NewStyle().Foreground(th.Over),
		night:       lipgloss.NewStyle().Foreground(th.Warn),
		weekend:     th.WeekendText,
		holiday:     th.HolidayText,
		empty:       th.Muted,
		band:        lipgloss.NewStyle().Foreground(bandFG).Background(bandBG),
		line:        lipgloss.NewStyle().Foreground(th.Accent).Background(bandBG),
		under:       lipgloss.NewStyle().Foreground(th.Under),
		on:          lipgloss.NewStyle().Foreground(th.On),
		over:        lipgloss.NewStyle().Foreground(th.Over),
		weekendText: th.WeekendText,
		holidayText: th.HolidayText,
		holidayName: th.HolidayText.Italic(true),
		faint:       th.Muted,
		bold:        th.Strong,
		label:       th.Label,
		active:      th.Active,
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
	case cellHoliday:
		return s.holiday
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
