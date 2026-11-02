package chart

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/heatmap"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
)

// Layout constants of RenderHeatmapText.
const (
	// HeatmapHeight is the number of lines the full layout draws: the title,
	// one row per weekday, the axis and the hour labels.
	HeatmapHeight = 1 + 7 + 1 + 1
	// HeatmapCompactHeight is the number of lines the compact layout draws:
	// the title with the legend, one row per weekday and the hour labels in
	// the axis row.
	HeatmapCompactHeight = 1 + 7 + 1
	// HeatmapMinWidth is the narrowest area that fits the weekday labels, the
	// axis and one cell per hour.
	HeatmapMinWidth = 3 + 1 + 24
)

// HeatmapLayout is the arrangement RenderHeatmapText chooses for an area.
type HeatmapLayout int

// Layouts of the heatmap, from none to the largest.
const (
	// HeatmapNone fits nowhere: the area is too small for any heatmap.
	HeatmapNone HeatmapLayout = iota
	// HeatmapCompact fits small windows (the body of an 80×24 terminal): the
	// hour labels share the axis row, the colour legend sits in the title.
	HeatmapCompact
	// HeatmapFull is the layout for larger windows, with its own axis row.
	HeatmapFull
)

// String implements fmt.Stringer.
func (l HeatmapLayout) String() string {
	switch l {
	case HeatmapNone:
		return "none"
	case HeatmapCompact:
		return "compact"
	case HeatmapFull:
		return "full"
	}
	return fmt.Sprintf("HeatmapLayout(%d)", int(l))
}

// Height is the number of lines the layout draws; 0 for HeatmapNone.
func (l HeatmapLayout) Height() int {
	switch l {
	case HeatmapCompact:
		return HeatmapCompactHeight
	case HeatmapFull:
		return HeatmapHeight
	}
	return 0
}

// HeatmapLayoutFor chooses the layout for an area of w × h cells: the full
// layout when it fits, the compact one when only that fits, and none below
// HeatmapCompactHeight lines or HeatmapMinWidth cells. The width only
// decides whether a heatmap fits at all: both layouts narrow their cells
// down to one per hour and thin out the hour labels on their own.
func HeatmapLayoutFor(w, h int) HeatmapLayout {
	switch {
	case w < HeatmapMinWidth || h < HeatmapCompactHeight:
		return HeatmapNone
	case h < HeatmapHeight:
		return HeatmapCompact
	}
	return HeatmapFull
}

// HeatScale is the colour scale of the heatmap from cool to hot: four
// shades of the first Palette colour, so the heatmap matches the bar chart.
var HeatScale = []color.Color{
	lipgloss.Color("#c6d5e6"),
	lipgloss.Color("#94afd0"),
	lipgloss.Color("#6b93bb"),
	Palette[0],
}

// heatRunes are the cell characters per HeatScale index, so the intensity
// is visible without colours too.
var heatRunes = []rune{'░', '▒', '▓', '█'}

var (
	// weekdayLabels are the row labels from the bottom up: the heatmap
	// counts rows from the bottom, and Monday is the top row.
	weekdayLabels = []string{"Sun", "Sat", "Fri", "Thu", "Wed", "Tue", "Mon"}
	hourLabels    = func() []string {
		labels := make([]string, 24)
		for h := range labels {
			labels[h] = fmt.Sprintf("%02d", h)
		}
		return labels
	}()
)

// RenderHeatmapText renders stats.ByWeekdayHour as a weekday × hour grid
// in the layout HeatmapLayoutFor picks for w × h: exactly that layout's
// Height lines, none wider than w cells. A title line names the metric and
// the maximum, one row per weekday follows from Monday at the top, the
// hours run along the bottom. A cell's intensity is its value relative to
// the maximum, drawn as a shade character in a colour of HeatScale (the
// same colour as background, so colour terminals show solid blocks and
// plain output the shades); cells without activity stay blank. The compact
// layout drops the axis row and shows the shades as a legend in the title
// instead. For HeatmapNone the result is "".
func RenderHeatmapText(stats store.Stats, metric Metric, w, h int) string {
	layout := HeatmapLayoutFor(w, h)
	if layout == HeatmapNone {
		return ""
	}
	maxValue := 0.0
	for d := range stats.ByWeekdayHour {
		for _, a := range stats.ByWeekdayHour[d] {
			maxValue = max(maxValue, metric.value(a))
		}
	}
	// The linechart underneath reserves an axis row and a label row below the
	// grid; the compact layout keeps the labels in the axis row and cuts the
	// label row off the view.
	hm := heatmap.New(w, HeatmapHeight-1,
		heatmap.WithXLabels(hourLabels),
		heatmap.WithYLabels(weekdayLabels),
		heatmap.WithCellSize(1, 1),
		heatmap.WithColorScale(HeatScale),
		heatmap.WithValueRange(0, max(maxValue, 1)),
	)
	// Y counts up from the bottom; Monday is the top row.
	for d := range stats.ByWeekdayHour {
		for hr, a := range stats.ByWeekdayHour[d] {
			if v := metric.value(a); v > 0 {
				hm.Push(heatmap.NewHeatPointInt(hr, 6-d, v))
			}
		}
	}
	hm.Draw()

	title := fmt.Sprintf("%s by weekday × hour · max %.0f", metric, maxValue)
	if layout == HeatmapFull {
		decorate(&hm)
		return ansi.Truncate(title, w, "…") + "\n" + hm.View()
	}
	decorateCompact(&hm)
	title = fmt.Sprintf("%s by weekday × hour · %s · max %.0f", metric, heatLegend(), maxValue)
	view := hm.View()
	view = view[:strings.LastIndex(view, "\n")]
	return ansi.Truncate(title, w, "…") + "\n" + view
}

// heatLegend is the colour ramp as one shade character per HeatScale index,
// each in its colour, for the title of the compact layout.
func heatLegend() string {
	var b strings.Builder
	for i, c := range HeatScale {
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render(string(heatRunes[i])))
	}
	return b.String()
}

// decorate moves the hour labels down to the last row, draws the axes in
// the row and column the heatmap leaves blank and gives every coloured
// cell the shade character of its scale index in the same colour.
func decorate(hm *heatmap.Model) {
	o := hm.Origin()
	for x := 0; x < hm.Width(); x++ {
		p := canvas.Point{X: x, Y: o.Y}
		hm.Canvas.SetCell(canvas.Point{X: x, Y: o.Y + 1}, hm.Canvas.Cell(p))
		hm.Canvas.SetRune(p, ' ')
	}
	shade(hm)
	hm.Canvas.SetRune(o, '┼')
	for x := o.X + 1; x < hm.Width(); x++ {
		hm.Canvas.SetRune(canvas.Point{X: x, Y: o.Y}, '─')
	}
}

// decorateCompact leaves the hour labels in the axis row, closes the
// vertical axis below the last weekday with a tick and gives every coloured
// cell the shade character of its scale index in the same colour.
func decorateCompact(hm *heatmap.Model) {
	shade(hm)
	hm.Canvas.SetRune(hm.Origin(), '╵')
}

// shade draws the vertical axis left of the grid and gives every coloured
// cell the shade character of its scale index in the same colour.
func shade(hm *heatmap.Model) {
	o := hm.Origin()
	top := o.Y - hm.GraphHeight()
	for y := top; y < o.Y; y++ {
		hm.Canvas.SetRune(canvas.Point{X: o.X, Y: y}, '│')
		for x := o.X + 1; x < hm.Width(); x++ {
			p := canvas.Point{X: x, Y: y}
			st := hm.Canvas.GetCellStyle(p)
			if st == nil {
				continue
			}
			if i := scaleIndex(st.GetBackground()); i >= 0 {
				hm.Canvas.SetRuneWithStyle(p, heatRunes[i], st.Foreground(HeatScale[i]))
			}
		}
	}
}

// scaleIndex is the index of c in HeatScale, or -1.
func scaleIndex(c color.Color) int {
	if c == nil {
		return -1
	}
	r, g, b, a := c.RGBA()
	for i, s := range HeatScale {
		if sr, sg, sb, sa := s.RGBA(); sr == r && sg == g && sb == b && sa == a {
			return i
		}
	}
	return -1
}
