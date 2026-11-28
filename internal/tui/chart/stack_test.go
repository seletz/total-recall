package chart

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestStackCells: the segments of a bar, scaled to cells, add up to the
// height of the bar, and every boundary between two segments lies on a
// whole cell, so no segment ends in a partial cell with another segment
// above it.
func TestStackCells(t *testing.T) {
	for _, tc := range []struct {
		values []float64
		sf     float64
	}{
		{[]float64{3, 4}, 0.37},
		{[]float64{9596.0 / 26, 4475.0 / 26}, 13.0 / 675},
		{[]float64{1, 1, 1, 1}, 0.3},
		{[]float64{10, 0.2, 5}, 0.5},
		{[]float64{0.1, 0.1}, 1},
		{[]float64{5}, 0.77},
		{[]float64{0, 7, 0}, 0.5},
	} {
		got := stackCells(tc.values, tc.sf)
		total := 0.0
		for _, v := range tc.values {
			total += v
		}
		sum, cum := 0.0, 0.0
		for i, h := range got {
			if h < 0 {
				t.Errorf("%v: segment %d has height %v", tc.values, i, h)
			}
			sum += h
			cum += h
			if i < len(got)-1 && cum != math.Round(cum) {
				t.Errorf("%v (sf %v): boundary after segment %d at %v cells, not a whole cell", tc.values, tc.sf, i, cum)
			}
		}
		if math.Abs(sum-total*tc.sf) > 1e-9 {
			t.Errorf("%v (sf %v): segments add up to %v cells, the bar is %v", tc.values, tc.sf, sum, total*tc.sf)
		}
	}
}

// TestStackedBarsHaveNoGaps: in a rendered chart every column of a bar is
// solid from the axis up to its top: only the top cell may be a partial
// block, and nothing empty lies between two segments.
func TestStackedBarsHaveNoGaps(t *testing.T) {
	spec := Spec{Title: "events", Labels: []string{"a", "b", "c", "d", "e", "f"}, Series: []Series{
		{Key: "x", Name: "host-a", Color: color.RGBA{R: 200, A: 255}, Values: []float64{369, 420, 211, 390, 105, 333}},
		{Key: "y", Name: "host-b", Color: color.RGBA{B: 200, A: 255}, Values: []float64{172, 33, 250, 101, 299, 17}},
		{Key: "z", Name: "other", Color: color.RGBA{G: 200, A: 255}, Values: []float64{13, 77, 5, 41, 66, 8}},
	}}
	lines := strings.Split(ansi.Strip(RenderText(spec, 60, 18)), "\n")
	// Rows of the bars: below the title, above the axis and the labels.
	bars := lines[1 : len(lines)-3]
	width := 0
	for _, l := range bars {
		width = max(width, len([]rune(l)))
	}
	for x := 0; x < width; x++ {
		var col []rune // bottom up
		for y := len(bars) - 1; y >= 0; y-- {
			r := []rune(bars[y])
			c := ' '
			if x < len(r) {
				c = r[x]
			}
			col = append(col, c)
		}
		if !strings.ContainsAny(string(col), "▁▂▃▄▅▆▇█") {
			continue
		}
		top := false
		for y, c := range col {
			full := c == '█'
			switch {
			case top && c != ' ':
				t.Errorf("column %d: %q at row %d above the top of its bar:\n%s", x, c, y, strings.Join(lines, "\n"))
			case !full && !top:
				top = true
			}
		}
	}
}
