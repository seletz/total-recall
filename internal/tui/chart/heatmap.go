package chart

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/heatmap"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
)

// Layout constants of RenderHeatmapText.
const (
	// HeatmapHeight is the number of lines RenderHeatmapText draws: the
	// title, one row per weekday, the axis and the hour labels.
	HeatmapHeight = 1 + 7 + 2
	// HeatmapMinWidth is the narrowest area that fits the weekday labels, the
	// axis and one cell per hour.
	HeatmapMinWidth = 3 + 1 + 24
)

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
// of exactly HeatmapHeight lines, none wider than w cells: a title line
// naming the metric and the maximum, one row per weekday from Monday at
// the top, the hours along the bottom. A cell's intensity is its value
// relative to the maximum, drawn as a shade character in a colour of
// HeatScale (the same colour as background, so colour terminals show solid
// blocks and plain output the shades); cells without activity stay blank.
// Below HeatmapHeight lines or HeatmapMinWidth cells the result is "".
func RenderHeatmapText(stats store.Stats, metric Metric, w, h int) string {
	if h < HeatmapHeight || w < HeatmapMinWidth {
		return ""
	}
	maxValue := 0.0
	for d := range stats.ByWeekdayHour {
		for _, a := range stats.ByWeekdayHour[d] {
			maxValue = max(maxValue, metric.value(a))
		}
	}
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
	decorate(&hm)

	title := fmt.Sprintf("%s by weekday × hour · max %.0f", metric, maxValue)
	return ansi.Truncate(title, w, "…") + "\n" + hm.View()
}

// decorate moves the hour labels down to the last row, draws the axes in
// the row and column the heatmap leaves blank and gives every coloured
// cell the shade character of its scale index in the same colour.
func decorate(hm *heatmap.Model) {
	o := hm.Origin()
	top := o.Y - hm.GraphHeight()
	for x := 0; x < hm.Width(); x++ {
		p := canvas.Point{X: x, Y: o.Y}
		hm.Canvas.SetCell(canvas.Point{X: x, Y: o.Y + 1}, hm.Canvas.Cell(p))
		hm.Canvas.SetRune(p, ' ')
	}
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
	hm.Canvas.SetRune(o, '┼')
	for x := o.X + 1; x < hm.Width(); x++ {
		hm.Canvas.SetRune(canvas.Point{X: x, Y: o.Y}, '─')
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
