// Package zone locates the clickable elements of a rendered line. A layout
// function returns, beside its text, the span of columns every element
// takes; the click handler looks the column up in those spans. Spans come
// from the same pass that renders the text, so hit-testing never drifts
// from what is shown, and a span is clipped to the width like the text, so
// an element cut off by a narrow terminal is not clickable.
package zone

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Span is the run of columns [Start, End) of one element on a row.
type Span struct {
	Start, End int
}

// Contains reports whether column x lies in the span.
func (s Span) Contains(x int) bool { return x >= s.Start && x < s.End }

// At returns the index of the first span that contains x, -1 without one.
func At(spans []Span, x int) int {
	for i, s := range spans {
		if s.Contains(x) {
			return i
		}
	}
	return -1
}

// Clip cuts the span to the columns before width w, like a line that is
// truncated to w cells: a span beyond w becomes empty.
func (s Span) Clip(w int) Span { return Span{Start: min(s.Start, w), End: min(s.End, w)} }

// Clip cuts every span to the columns before width w (see Span.Clip).
func Clip(spans []Span, w int) []Span {
	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = s.Clip(w)
	}
	return out
}

// Rect is a rectangle of cells: W columns from X, H rows from Y.
type Rect struct {
	X, Y, W, H int
}

// Contains reports whether the cell (x, y) lies in the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Toggles renders names one space apart, the active one (index active, none
// when out of range) in brackets and the style st, and returns the text and
// the span of every name, counted from column offset, where the text starts
// on its row.
func Toggles(st lipgloss.Style, names []string, active, offset int) (string, []Span) {
	out := make([]string, len(names))
	spans := make([]Span, len(names))
	x := offset
	for i, n := range names {
		if i == active {
			n = "[" + n + "]"
			out[i] = st.Render(n)
		} else {
			out[i] = n
		}
		w := lipgloss.Width(n)
		spans[i] = Span{Start: x, End: x + w}
		x += w + 1
	}
	return strings.Join(out, " "), spans
}
