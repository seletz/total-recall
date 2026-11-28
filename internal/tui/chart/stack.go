package chart

import "math"

// stackCells returns the heights in cells of the segments of one stacked
// bar, values bottom up, at sf cells per unit. The cumulative heights are
// rounded to whole cells, at most to the whole cells of the bar, and the
// segments are their differences: every boundary between two segments
// lies on a whole cell and only the top of the bar keeps its fraction.
// Rounding each segment on its own would end a segment in a partial
// block with the next one starting a cell higher, a gap in the bar. A
// segment shorter than half a cell can vanish; the bar keeps its height.
func stackCells(values []float64, sf float64) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 {
		return out
	}
	total := 0.0
	for _, v := range values {
		total += max(v, 0)
	}
	top := total * sf
	ceiling := math.Floor(top)
	cum, prev := 0.0, 0.0
	for i, v := range values {
		cum += max(v, 0)
		b := top
		if i < len(values)-1 {
			b = min(math.Round(cum*sf), ceiling)
		}
		out[i] = b - prev
		prev = b
	}
	return out
}

// boundaryNudge lifts every inner boundary a little above its whole cell,
// so the float arithmetic of the bar chart never turns a full cell into a
// 7/8 block.
const boundaryNudge = 1e-6

// stackValues maps the segments of one bar to the values the bar chart
// draws (see stackCells).
func stackValues(values []float64, sf float64) []float64 {
	out := make([]float64, len(values))
	if sf <= 0 {
		copy(out, values)
		return out
	}
	cells := stackCells(values, sf)
	cum, prev := 0.0, 0.0
	for i, c := range cells {
		cum += c
		b := cum
		if i < len(cells)-1 && c > 0 {
			b += boundaryNudge
		}
		out[i] = max(b-prev, 0) / sf
		prev = max(b, prev)
	}
	return out
}
