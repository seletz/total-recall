// Package theme is the shared look of the TUI: one palette for a dark and
// one for a light terminal background, and the semantic styles every view
// draws with (accent, muted, the actors, matches, ratings, holidays), so
// the views look alike. The hues are the series colours of the charts
// (chart.Palette): the theme assigns them roles and adapts them to the
// background, darkened on a light one (Tone). Everything coloured must
// also differ in glyph, weight or position, so the views stay readable
// with NO_COLOR and in a colourless golden file; the renderer drops the
// colours on such terminals by itself.
//
// Views start with the dark theme (most terminals are dark) and switch
// when the terminal answers the app's tea.RequestBackgroundColor with a
// tea.BackgroundColorMsg, which the app broadcasts to every view.
package theme

import (
	"image/color"
	"math"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/tui/chart"
)

// Theme is the palette and the styles for one background. Build it with
// New; the zero value is unstyled.
type Theme struct {
	// Dark is the background the theme is for.
	Dark bool

	// Palette: the colours by role. Accent is the primary colour of the
	// chrome (title, active tab, labels, borders); Accent2 and Accent3
	// are the further stops of the title gradient.
	Accent, Accent2, Accent3 color.Color
	// Actors of the transcripts.
	Human, Agent, Peer, Tool color.Color
	// Highlight marks search matches; Warn errors and risks.
	Highlight, Warn color.Color
	// Days off: a public holiday and a weekend.
	Holiday, Weekend color.Color
	// Ratings of a span against the working time: under the bounds,
	// within them, over them.
	Under, On, Over color.Color
	// Selection is the background of a selected row or match.
	Selection color.Color

	// Styles. Title is the bold accent of headings and names, Label the
	// bold accent of row labels, Strong plain bold, Muted the faint text
	// of chrome and secondary lines, Rule the colour of rules and
	// separators, Border the rounded border of panels.
	Title, Label, Strong, Muted, Rule, Border lipgloss.Style
	// Active is an active tab or toggle (accent background), Mark the
	// marker glyphs ▸ and ◄, Match a search match, SelectedMatch the
	// selected match (reversed, so it stands out without colour too) and
	// Selected the marked row of a focused list (selection background).
	Active, Mark, Match, SelectedMatch, Selected lipgloss.Style
	// AccentText is plain text in the accent colour; Error an error.
	AccentText, Error lipgloss.Style
	// Figure is a key figure (tiles of the stats view, spans of the
	// activity view).
	Figure lipgloss.Style
	// HolidayText and WeekendText colour the rows and names of days off.
	HolidayText, WeekendText lipgloss.Style
	// Spinner is the spinner glyph while a view loads.
	Spinner lipgloss.Style
	// Added and Removed colour the lines of a diff: green and red text,
	// next to the + and - at their start, so a diff reads without colour.
	Added, Removed lipgloss.Style
}

// New returns the theme for a dark or a light background.
func New(dark bool) Theme {
	t := Theme{Dark: dark}
	ld := lipgloss.LightDark(dark)
	t.Accent = t.Tone(chart.Palette[0])
	t.Accent2 = t.Tone(chart.Palette[5])
	t.Accent3 = t.Tone(chart.Palette[4])
	t.Human = t.Tone(chart.Palette[1])
	t.Agent = t.Tone(chart.Palette[0])
	t.Peer = t.Tone(chart.Palette[4])
	t.Tool = t.Tone(chart.Palette[5])
	t.Highlight = ld(lipgloss.Color("#9a6f00"), lipgloss.Color("#f2c84b"))
	t.Warn = t.Tone(chart.Palette[3])
	t.Holiday = t.Tone(chart.HolidayColor)
	t.Weekend = t.Tone(chart.Palette[4])
	t.Under = t.Tone(chart.Palette[5])
	t.On = t.Tone(chart.Palette[2])
	t.Over = t.Tone(chart.Palette[1])
	t.Selection = ld(lipgloss.Color("#dbe5f1"), lipgloss.Color("#243447"))

	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	t.Title = fg(t.Accent).Bold(true)
	t.Label = fg(t.Accent).Bold(true)
	t.Strong = lipgloss.NewStyle().Bold(true)
	t.Muted = lipgloss.NewStyle().Faint(true)
	t.Rule = fg(ld(lipgloss.Color("#9fb3c8"), lipgloss.Color("#3a5b82")))
	t.Border = fg(t.Accent)
	t.Active = lipgloss.NewStyle().Background(t.Accent).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	t.Mark = fg(t.Accent).Bold(true)
	t.Match = fg(t.Highlight).Bold(true).Underline(true)
	t.SelectedMatch = t.Match.Reverse(true)
	t.Selected = lipgloss.NewStyle().Background(t.Selection).Bold(true)
	t.AccentText = fg(t.Accent)
	t.Error = fg(t.Warn)
	t.Figure = fg(t.Accent2).Bold(true)
	t.HolidayText = fg(t.Holiday)
	t.WeekendText = fg(t.Weekend)
	t.Spinner = fg(t.Accent2)
	t.Added = fg(t.On)
	t.Removed = fg(t.Warn)
	return t
}

// Tone adapts a palette colour to the background: as is on a dark one,
// darkened on a light one, where the mid-tone series colours lack
// contrast against white.
func (t Theme) Tone(c color.Color) color.Color {
	if t.Dark {
		return c
	}
	return lipgloss.Darken(c, 0.25)
}

// minContrast is the contrast ratio (WCAG, the level for normal text) a
// colour keeps on the selection background.
const minContrast = 4.5

// OnSelection returns c for text on the Selection background: as is when
// it reads there, else moved towards white on a dark theme and black on a
// light one just far enough, so it keeps its hue as far as possible. Pass
// a toned colour (Tone).
func (t Theme) OnSelection(c color.Color) color.Color {
	move := lipgloss.Darken
	if t.Dark {
		move = lipgloss.Lighten
	}
	for step := 0.0; step < 1; step += 0.05 {
		if got := move(c, step); contrast(got, t.Selection) >= minContrast {
			return got
		}
	}
	return move(c, 1)
}

// contrast is the WCAG contrast ratio of two colours, 1 to 21.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// luminance is the relative luminance of a colour, 0 to 1.
func luminance(c color.Color) float64 {
	channel := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	r, g, b, _ := c.RGBA()
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// Gradient renders s bold with its runes blended from Accent over Accent2
// to Accent3, the welcome of the header. One or two runes are plain Title.
func (t Theme) Gradient(s string) string {
	runes := []rune(s)
	if len(runes) < 3 {
		return t.Title.Render(s)
	}
	colours := lipgloss.Blend1D(len(runes), t.Accent, t.Accent2, t.Accent3)
	var b strings.Builder
	for i, r := range runes {
		b.WriteString(t.Title.Foreground(colours[i]).Render(string(r)))
	}
	return b.String()
}

// Actor returns the style of an actor's label: bold in the actor's
// colour; an unknown actor is plain.
func (t Theme) Actor(a model.Actor) lipgloss.Style {
	st := lipgloss.NewStyle().Bold(true)
	switch a {
	case model.ActorHuman:
		return st.Foreground(t.Human)
	case model.ActorAgent:
		return st.Foreground(t.Agent)
	case model.ActorPeer:
		return st.Foreground(t.Peer)
	case model.ActorTool, model.ActorAutomation:
		return st.Foreground(t.Tool)
	}
	return lipgloss.NewStyle()
}

// Help returns the styles of the help widget: keys in the accent, the
// descriptions and separators muted.
func (t Theme) Help() help.Styles {
	ld := lipgloss.LightDark(t.Dark)
	key := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	desc := lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#5f6b7a"), lipgloss.Color("#9aa5b1")))
	sep := lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#b8c2cc"), lipgloss.Color("#4a5563")))
	return help.Styles{
		Ellipsis:       sep,
		ShortKey:       key,
		ShortDesc:      desc,
		ShortSeparator: sep,
		FullKey:        key,
		FullDesc:       desc,
		FullSeparator:  sep,
	}
}
