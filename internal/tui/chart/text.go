package chart

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/barchart"
	"github.com/charmbracelet/x/ansi"
)

// Layout constants of RenderText.
const (
	minHeight   = 5 // title, one bar row, axis, axis labels, legend
	minBarWidth = 1
	maxBarWidth = 8
	barGap      = 1
)

// RenderText renders spec as a stacked text bar chart of exactly h lines,
// none wider than w cells: a title line, the chart with a value axis on the
// left (the maximum at the top, 0 at the axis) and period labels below, and
// a legend line. Segments are coloured with lipgloss; strip the escape
// sequences for plain output. Bars are left-aligned; when they do not all
// fit, the last periods are cut off. Below minHeight lines or without room
// for a bar the result is "".
func RenderText(spec Spec, w, h int) string {
	n := len(spec.Labels)
	maxValue := 0.0
	for j := 0; j < n; j++ {
		sum := 0.0
		for _, s := range spec.Series {
			if j < len(s.Values) {
				sum += s.Values[j]
			}
		}
		maxValue = max(maxValue, sum)
	}
	maxLabel := ""
	if maxValue > 0 {
		maxLabel = fmt.Sprintf("%.0f", maxValue)
	}
	gutter := max(len(maxLabel), 1) + 2 // number, space, axis
	cw := w - gutter
	if h < minHeight || cw < 1 {
		return ""
	}
	chartH := h - 2 // title and legend

	bw, gap := minBarWidth, barGap
	if n > 0 {
		bw = min(max((cw-(n-1)*gap)/n, minBarWidth), maxBarWidth)
	}
	if bw*n+gap*(n-1) > cw {
		gap = 0 // too many bars for a gap; the rest is cut off anyway
	}
	bc := barchart.New(cw, chartH, barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(bw), barchart.WithBarGap(gap))
	for j := 0; j < n; j++ {
		bd := barchart.BarData{Values: make([]barchart.BarValue, 0, len(spec.Series))}
		for _, s := range spec.Series {
			v := 0.0
			if j < len(s.Values) {
				v = s.Values[j]
			}
			bd.Values = append(bd.Values, barchart.BarValue{Name: s.Name, Value: v, Style: lipgloss.NewStyle().Foreground(s.Color)})
		}
		bc.Push(bd)
	}
	bc.Draw()
	rows := strings.Split(bc.View(), "\n")
	for len(rows) < chartH {
		rows = append(rows, "")
	}
	axisRow := chartH - 2
	rows[chartH-1] = axisLabels(spec.Labels, cw, bw, gap)
	for i := range rows {
		var left string
		switch i {
		case 0:
			if maxLabel != "" && axisRow > 0 {
				left = fmt.Sprintf("%*s ┤", gutter-2, maxLabel)
			} else {
				left = fmt.Sprintf("%*s │", gutter-2, "")
			}
		case axisRow:
			left = fmt.Sprintf("%*s ┼", gutter-2, "0")
		case chartH - 1:
			left = strings.Repeat(" ", gutter)
		default:
			left = fmt.Sprintf("%*s │", gutter-2, "")
		}
		rows[i] = left + rows[i]
	}

	var out strings.Builder
	out.WriteString(ansi.Truncate(spec.Title, w, "…"))
	out.WriteByte('\n')
	out.WriteString(strings.Join(rows, "\n"))
	out.WriteByte('\n')
	out.WriteString(ansi.Truncate(legend(spec), w, "…"))
	return out.String()
}

// axisLabels lays the period labels under the bars: every k-th bar gets its
// label at the bar's first column, where k is the smallest stride that
// keeps the labels apart. Labels that would exceed width are dropped.
func axisLabels(labels []string, width, barWidth, gap int) string {
	row := []rune(strings.Repeat(" ", width))
	longest := 0
	for _, l := range labels {
		longest = max(longest, len([]rune(l)))
	}
	stride := barWidth + gap
	every := (longest + 1 + stride - 1) / stride
	for i, l := range labels {
		if i%every != 0 {
			continue
		}
		x := i * stride
		r := []rune(l)
		if x+len(r) > width {
			break
		}
		copy(row[x:], r)
	}
	return strings.TrimRight(string(row), " ")
}

// legend joins a coloured swatch and the name of every series on one line.
func legend(spec Spec) string {
	parts := make([]string, 0, len(spec.Series))
	for _, s := range spec.Series {
		parts = append(parts, lipgloss.NewStyle().Foreground(s.Color).Render("██")+" "+s.Name)
	}
	return strings.Join(parts, "  ")
}
