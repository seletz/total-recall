// Package figure builds the charts of `total-recall chart` for monthly
// reports as canvas scenes: a calendar heatmap of the own messages per
// day, the share of agent work and own attention per project, a punchcard
// of weekday × hour and the working time per week and category. The
// functions are pure: the CLI gathers the data from the store and the
// configuration, the package lays it out, canvas writes SVG or PNG.
//
// Sizes follow the figures of a monthly report: about 360×290 (calendar),
// 610×370 (projects), 760×290 (punchcard) and 560×300 (working time) CSS
// pixels at their natural size; Encode scales them.
package figure

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/seletz/total-recall/internal/canvas"
	"github.com/seletz/total-recall/internal/project"
)

// Options apply to every figure.
type Options struct {
	// Period is the range the figure shows, appended to the title ("October
	// 2026", "2026-10-01 – 2026-10-15"); see PeriodName.
	Period string
	// Title replaces the heading, including the period, when set.
	Title string
	// Lang is the language of the labels; empty is EN.
	Lang Lang
	// NoTitle leaves the heading out, NoLegend the legend.
	NoTitle, NoLegend bool
	// Transparent leaves the background out; the default is white.
	Transparent bool
}

// Theme colours shared by the figures: text, the empty cells of the
// heatmaps, grid lines and the background.
var (
	TextColor  = canvas.Hex("#6b6b6b")
	EmptyColor = canvas.Hex("#e6e6e6")
	GridColor  = canvas.Hex("#d9d9d9")
	Background = canvas.Hex("#ffffff")
	// HumanColor and AgentColor mark the own messages and the agent
	// activity of the punchcard; the legend of the project chart uses
	// neutral swatches instead.
	HumanColor = canvas.Hex("#59a14f")
	AgentColor = canvas.Hex("#f28e2b")
	// TargetColor is the target line of the working time.
	TargetColor = canvas.Hex("#444444")
)

// Text sizes in CSS pixels.
const (
	titleSize = 14
	labelSize = 11
	smallSize = 10
)

// DefaultHeatmap is the built-in scale of the calendar, four steps from
// light to dark; the configuration ([chart] heatmap) replaces it.
var DefaultHeatmap = []string{"#c6dbef", "#9ecae1", "#4292c6", "#08519c"}

// Format is the output format of Encode.
type Format string

// Formats of Encode.
const (
	SVG Format = "svg"
	PNG Format = "png"
)

// ParseFormat returns the Format named by s: svg or png.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case SVG, PNG:
		return Format(s), nil
	}
	return "", fmt.Errorf("invalid format %q (svg, png)", s)
}

// Encode writes a scene in the format at the size.
func Encode(s canvas.Scene, f Format, size canvas.Size) ([]byte, error) {
	switch f {
	case SVG:
		return canvas.SVG(s, size), nil
	case PNG:
		return canvas.PNG(s, size)
	}
	return nil, fmt.Errorf("figure: unknown format %q", f)
}

// Colors parses a list of configured colours (names of project.Palette or
// #rrggbb) for a heatmap scale.
func Colors(hexes []string) ([]color.NRGBA, error) {
	out := make([]color.NRGBA, 0, len(hexes))
	for _, h := range hexes {
		p, err := project.ParseColor(h)
		if err != nil {
			return nil, err
		}
		if p == "" {
			return nil, fmt.Errorf("empty colour")
		}
		out = append(out, canvas.Hex(p))
	}
	return out, nil
}

// AssignColors gives every name a colour: the fixed one (#rrggbb) where
// there is one, otherwise the next colour of project.Palette in order of
// the names, skipping colours that are fixed for another name while any
// is free and cycling when the palette is exhausted.
func AssignColors(names []string, fixed map[string]string) map[string]color.NRGBA {
	out := make(map[string]color.NRGBA, len(names))
	taken := map[string]bool{}
	for _, n := range names {
		if h := fixed[n]; h != "" {
			taken[h] = true
		}
	}
	next := 0
	for _, n := range names {
		if h := fixed[n]; h != "" {
			out[n] = canvas.Hex(h)
			continue
		}
		var pick string
		for tries := 0; tries < len(project.Palette); tries++ {
			c := project.Palette[next%len(project.Palette)].Hex
			next++
			if !taken[c] {
				pick = c
				break
			}
		}
		if pick == "" {
			pick = project.Palette[next%len(project.Palette)].Hex
			next++
		}
		taken[pick] = true
		out[n] = canvas.Hex(pick)
	}
	return out
}

// PeriodName names a range [since, until) for a title in lang: a whole
// calendar month as "October 2026" ("Oktober 2026"), any other range as
// "2026-10-01 – 2026-10-15" ("01.10.2026 – 15.10.2026", the last day
// included). A zero bound is open ("… – 2026-10-15").
func PeriodName(since, until time.Time, loc *time.Location, lang Lang) string {
	if loc == nil {
		loc = time.UTC
	}
	if m, ok := wholeMonth(since, until, loc); ok {
		return lang.monthName(m)
	}
	return dayRange(since, until, loc, " – ", lang.day)
}

// PeriodSlug names a range for a file name: "2026-10" for a whole month,
// else "2026-10-01_2026-10-15".
func PeriodSlug(since, until time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	if m, ok := wholeMonth(since, until, loc); ok {
		return m.Format("2006-01")
	}
	return dayRange(since, until, loc, "_", func(t time.Time) string { return t.Format("2006-01-02") })
}

// wholeMonth reports whether [since, until) is a calendar month in loc,
// bounds taken by their local calendar day (a day boundary at 04:00 still
// counts).
func wholeMonth(since, until time.Time, loc *time.Location) (time.Time, bool) {
	if since.IsZero() || until.IsZero() {
		return time.Time{}, false
	}
	s, u := since.In(loc), until.In(loc)
	if s.Day() != 1 || u.Day() != 1 {
		return time.Time{}, false
	}
	first := time.Date(s.Year(), s.Month(), 1, 0, 0, 0, 0, loc)
	if u.Year() != first.AddDate(0, 1, 0).Year() || u.Month() != first.AddDate(0, 1, 0).Month() {
		return time.Time{}, false
	}
	return first, true
}

// dayRange formats the first and the last local day of [since, until)
// with day.
func dayRange(since, until time.Time, loc *time.Location, sep string, day func(time.Time) string) string {
	a, b := "…", "…"
	if !since.IsZero() {
		a = day(since.In(loc))
	}
	if !until.IsZero() {
		b = day(until.In(loc).Add(-time.Second))
	}
	return a + sep + b
}

// heading adds the title line and returns the height it takes (0 without
// a title). Options.Title replaces the default title and the period.
func heading(s *canvas.Scene, o Options, title string) float64 {
	if o.NoTitle {
		return 0
	}
	switch {
	case o.Title != "":
		title = o.Title
	case o.Period != "":
		title += " · " + o.Period
	}
	s.Add(canvas.Text{X: 0, Y: 16, Text: title, Size: titleSize, Bold: true, Fill: TextColor})
	return 22
}

// background returns the scene background for the options.
func background(o Options) color.NRGBA {
	if o.Transparent {
		return canvas.Transparent
	}
	return Background
}

// label is a small text item in the text colour.
func label(x, y float64, text string, anchor canvas.Anchor) canvas.Text {
	return canvas.Text{X: x, Y: y, Text: text, Size: labelSize, Fill: TextColor, Anchor: anchor}
}

// cut shortens s to n runes, ending in an ellipsis.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// itoa formats an integer.
func itoa(n int) string { return strconv.Itoa(n) }

// percent formats a share as "12 %".
func percent(v, total int) string {
	if total == 0 {
		return "0 %"
	}
	return fmt.Sprintf("%d %%", int(math.Round(100*float64(v)/float64(total))))
}
