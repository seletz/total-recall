package configview

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// rowStyle draws one row of a list or a form. A marked row sets its
// background on every segment: a styled segment ends with a reset, so a
// style around the finished row would stop at the first one. Colours on a
// marked row are moved to read on its background (theme.OnSelection).
type rowStyle struct {
	th     theme.Theme
	marked bool
	// text is the plain text of the row, muted its secondary text, strong
	// its bold text.
	text, muted, strong lipgloss.Style
}

// newRowStyle returns the style of a row, marked or not.
func newRowStyle(th theme.Theme, marked bool) rowStyle {
	if marked {
		return rowStyle{th: th, marked: true, text: th.Selected, muted: th.Selected.Bold(false).Faint(true), strong: th.Selected}
	}
	return rowStyle{th: th, text: lipgloss.NewStyle(), muted: th.Muted, strong: th.Strong}
}

// on is the style of text in the theme colour c, already toned: on a
// marked row c is moved to read on the selection.
func (s rowStyle) on(c color.Color) lipgloss.Style {
	if s.marked {
		c = s.th.OnSelection(c)
	}
	return s.text.Foreground(c)
}

// name is the style of a project name in colour c, toned for the
// background and legible on a marked row; nil is the plain text.
func (s rowStyle) name(c color.Color) lipgloss.Style {
	if c == nil {
		return s.text
	}
	return s.on(s.th.Tone(c))
}

// swatch is the colour field of a definition: a block in its colour c
// when one is configured, a dot for the automatic colour.
func (s rowStyle) swatch(configured string, c color.Color) string {
	if hex, err := project.ParseColor(configured); err != nil || hex == "" {
		return s.muted.Render("·")
	}
	return s.name(c).Render("■")
}

// fill pads a row to width in the style of the row.
func (s rowStyle) fill(line string, width int) string {
	if !s.marked {
		return line
	}
	return line + s.text.Render(strings.Repeat(" ", max(width-lipgloss.Width(line), 0)))
}
