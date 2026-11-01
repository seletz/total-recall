package chart

import (
	"errors"
	"fmt"
	"image/color"
	"strconv"

	"github.com/go-analyze/charts"
)

// CellSize is the size of one terminal cell in pixels, as the terminal
// reports it on CSI 16 t. The zero value means unknown; every helper then
// falls back to DefaultCellSize per side.
type CellSize struct {
	Width  int
	Height int
}

// DefaultCellSize is the cell size assumed when the terminal does not report
// one. It matches the fallback of chartpicture.
var DefaultCellSize = CellSize{Width: 10, Height: 20}

// Text sizes of the picture, relative to the cell height.
const (
	fontCellRatio = 0.6 // glyph height as a share of the cell height
	minFontSize   = 10.0
)

// Picture theme colours for a dark terminal background. The background is
// transparent, so the terminal's own background shows through under Kitty
// Graphics; the text is light to stay readable on it.
var (
	imageTextColor      = charts.Color{R: 0xd0, G: 0xd0, B: 0xd0, A: 255}
	imageAxisColor      = charts.Color{R: 0x8c, G: 0x8c, B: 0x8c, A: 255}
	imageSplitLineColor = charts.Color{R: 0x40, G: 0x40, B: 0x40, A: 255}
)

// or returns v, or fallback when v is not positive.
func or(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

// PixelSize returns the pixel size of an area of cols × rows terminal cells.
// A non-positive cell dimension falls back to DefaultCellSize; a
// non-positive cols or rows yields 0 on that side.
func PixelSize(cols, rows int, cell CellSize) (w, h int) {
	w = max(cols, 0) * or(cell.Width, DefaultCellSize.Width)
	h = max(rows, 0) * or(cell.Height, DefaultCellSize.Height)
	return w, h
}

// FontSize returns the text size in pixels that fits the cell height: about
// the x-height of the terminal font, at least minFontSize. For
// DefaultCellSize it is the go-analyze/charts default of 12.
func (c CellSize) FontSize() float64 {
	return max(float64(or(c.Height, DefaultCellSize.Height))*fontCellRatio, minFontSize)
}

// toChartColor converts any image/color.Color into the go-analyze/charts
// colour, keeping the alpha channel; nil is transparent.
func toChartColor(c color.Color) charts.Color {
	if c == nil {
		return charts.ColorTransparent
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return charts.Color{R: n.R, G: n.G, B: n.B, A: n.A}
}

// ToBarChartOption turns spec into a stacked go-analyze/charts bar chart
// with the same colours as RenderText, a title, a legend and both axes. The
// text is sized for DefaultCellSize; see ToBarChartOptionAt for other cell
// sizes. The option carries its own Theme, so a renderer that sets a default
// theme of its own, such as chartpicture, still shows the spec's colours.
func ToBarChartOption(spec Spec) charts.BarChartOption {
	return ToBarChartOptionAt(spec, DefaultCellSize)
}

// ToBarChartOptionAt is ToBarChartOption with the text sized for terminal
// cells of size cell, so that labels stay readable at high pixel densities.
func ToBarChartOptionAt(spec Spec, cell CellSize) charts.BarChartOption {
	series := make(charts.BarSeriesList, 0, len(spec.Series))
	names := make([]string, 0, len(spec.Series))
	colors := make([]charts.Color, 0, len(spec.Series))
	for _, s := range spec.Series {
		values := make([]float64, len(spec.Labels))
		copy(values, s.Values)
		series = append(series, charts.BarSeries{Name: s.Name, Values: values})
		names = append(names, s.Name)
		colors = append(colors, toChartColor(s.Color))
	}
	theme := charts.MakeTheme(charts.ThemeOption{
		IsDarkMode:         true,
		AxisStrokeColor:    imageAxisColor,
		AxisSplitLineColor: imageSplitLineColor,
		BackgroundColor:    charts.ColorTransparent,
		TextColor:          imageTextColor,
		SeriesColors:       colors,
	})
	font := charts.FontStyle{FontSize: cell.FontSize(), FontColor: imageTextColor}

	opt := charts.NewBarChartOptionWithSeries(series)
	opt.Theme = theme
	opt.Padding = charts.NewBoxEqual(int(cell.FontSize()))
	opt.StackSeries = charts.Ptr(true)
	opt.Title = charts.TitleOption{Text: spec.Title, FontStyle: font}
	opt.Legend = charts.LegendOption{SeriesNames: names, FontStyle: font, Symbol: charts.SymbolSquare}
	opt.CategoryAxis = charts.CategoryAxisOption{Labels: spec.Labels, LabelFontStyle: font}
	opt.ValueAxis = []charts.ValueAxisOption{{LabelFontStyle: font, SplitLineShow: charts.Ptr(true)}}
	opt.ValueFormatter = formatValue
	return opt
}

// formatValue prints axis values as counts: 70 rather than 70.00, with
// decimals only where a tick falls between integers.
func formatValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// RenderPNG renders spec as a stacked bar chart into a PNG of exactly
// widthPx × heightPx pixels with a transparent background. Use PixelSize to
// get the size of a cell area. An empty spec renders the title and the
// library's "no data" mark.
func RenderPNG(spec Spec, widthPx, heightPx int) ([]byte, error) {
	if widthPx <= 0 || heightPx <= 0 {
		return nil, fmt.Errorf("chart: picture size %d×%d must be positive", widthPx, heightPx)
	}
	opt := ToBarChartOption(spec)
	p := charts.NewPainter(charts.PainterOptions{
		OutputFormat: charts.ChartOutputPNG,
		Width:        widthPx,
		Height:       heightPx,
		Theme:        opt.Theme,
	})
	if err := p.BarChart(opt); err != nil {
		return nil, fmt.Errorf("chart: render bar chart: %w", err)
	}
	data, err := p.Bytes()
	if err != nil {
		return nil, fmt.Errorf("chart: encode png: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("chart: empty png")
	}
	return data, nil
}
