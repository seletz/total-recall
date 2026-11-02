// Package chart turns a store.ActivitySeries into a renderer-neutral chart
// specification and renders it, for now as text through ntcharts. The
// picture variant for Kitty Graphics renders the same Spec.
package chart

import (
	"fmt"
	"image/color"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/store"
)

// Metric is the number a chart shows per period and series.
type Metric int

// Metrics of a Spec.
const (
	MetricEvents   Metric = iota // human plus agent events
	MetricHuman                  // human events only
	MetricSessions               // distinct sessions
)

// String returns events, human or sessions; it is the chart title.
func (m Metric) String() string {
	switch m {
	case MetricEvents:
		return "events"
	case MetricHuman:
		return "human"
	case MetricSessions:
		return "sessions"
	}
	return fmt.Sprintf("Metric(%d)", int(m))
}

// value returns the metric of one activity cell.
func (m Metric) value(a store.Activity) float64 {
	switch m {
	case MetricHuman:
		return float64(a.Human)
	case MetricSessions:
		return float64(a.Sessions)
	}
	return float64(a.Human + a.Agent)
}

// Palette holds the series colours in assignment order. The text and the
// picture renderer use the same colours, so a series looks alike in both.
var Palette = []color.Color{
	lipgloss.Color("#4e79a7"), // blue
	lipgloss.Color("#f28e2b"), // orange
	lipgloss.Color("#59a14f"), // green
	lipgloss.Color("#e15759"), // red
	lipgloss.Color("#b07aa1"), // purple
	lipgloss.Color("#76b7b2"), // teal
}

// OtherColor is the colour of the folded series store.OtherKey.
var OtherColor = lipgloss.Color("#8c8c8c")

// Series is one stacked segment across all periods.
type Series struct {
	Key    string      // raw dimension value, store.OtherKey for the folded rest
	Name   string      // display name, the legend entry
	Color  color.Color // segment colour
	Values []float64   // one value per Spec.Labels entry
}

// Spec is a stacked bar chart: one bar per period, one segment per series.
// It carries no rendering state, so text and picture renderers share it.
type Spec struct {
	Title  string       // the metric name
	Period store.Period // period of the labels
	Labels []string     // axis label per bar, see AxisLabel
	Series []Series     // bottom segment first; store.OtherKey last
	// Marks say per label which days are off: one per label for a day
	// period, else nil. The text renderer colours the axis labels by them;
	// the picture renderer has no coloured labels and ignores them.
	Marks []Mark
}

// Mark is what sets a day apart on the axis: a weekend or a public
// holiday (named).
type Mark struct {
	Weekend bool
	Holiday string
}

// IsZero reports whether the day is an ordinary workday.
func (m Mark) IsZero() bool { return m == Mark{} }

// HolidayColor is the colour of a holiday's axis label; it is no series
// colour.
var HolidayColor = lipgloss.Color("#e377c2")

// DayMarks returns the marks of the buckets of a day series: the weekend
// days and the holidays named by holidays (nil names none). For another
// period it returns nil.
func DayMarks(s store.ActivitySeries, period store.Period, holidays store.Holidays) []Mark {
	if period != store.PeriodDay || len(s.Buckets) == 0 {
		return nil
	}
	marks := make([]Mark, len(s.Buckets))
	for i, b := range s.Buckets {
		marks[i].Weekend = store.IsWeekend(b.Start)
		if holidays != nil {
			marks[i].Holiday = holidays.Name(b.Start)
		}
	}
	return marks
}

// AxisLabel formats the start of a period for the chart axis: days as 05,
// ISO weeks as W41, months as Oct.
func AxisLabel(start time.Time, p store.Period) string {
	switch p {
	case store.PeriodWeek:
		_, w := start.ISOWeek()
		return fmt.Sprintf("W%02d", w)
	case store.PeriodMonth:
		return start.Format("Jan")
	}
	return start.Format("02")
}

// FromSeries builds the Spec of a series aggregated per period, for one
// metric. Series keep the order of s.Keys (by volume, OtherKey last) and
// are named through names; a key without a name is shown as is. Colours
// come from Palette in series order, cycling when there are more series
// than colours; OtherKey is always OtherColor. For colours that survive a
// change of period or dimension, pass the result through Colors.Assign.
func FromSeries(s store.ActivitySeries, period store.Period, metric Metric, names map[string]string) Spec {
	spec := Spec{Title: metric.String(), Period: period}
	for _, b := range s.Buckets {
		spec.Labels = append(spec.Labels, AxisLabel(b.Start, spec.Period))
	}
	for i, k := range s.Keys {
		sr := Series{Key: k, Name: k, Color: Palette[i%len(Palette)]}
		if n, ok := names[k]; ok {
			sr.Name = n
		}
		if k == store.OtherKey {
			sr.Color = OtherColor
		}
		if len(s.Buckets) > 0 {
			sr.Values = make([]float64, len(s.Buckets))
		}
		for j, b := range s.Buckets {
			if i < len(b.Cells) {
				sr.Values[j] = metric.value(b.Cells[i].Activity)
			}
		}
		spec.Series = append(spec.Series, sr)
	}
	return spec
}

// Colors remembers which colour each series key received, so a series
// keeps its colour when the user switches period, dimension or metric and
// the order of the series changes. The zero value is ready to use; one
// Colors should live as long as the view.
type Colors struct {
	byKey map[string]color.Color
	used  int
}

// Assign returns a copy of spec whose series carry their remembered colour;
// a key seen for the first time takes the next Palette colour, cycling
// when the palette is exhausted. store.OtherKey is always OtherColor.
func (c *Colors) Assign(spec Spec) Spec {
	if c.byKey == nil {
		c.byKey = map[string]color.Color{}
	}
	out := spec
	out.Series = make([]Series, len(spec.Series))
	copy(out.Series, spec.Series)
	for i := range out.Series {
		sr := &out.Series[i]
		if sr.Key == store.OtherKey {
			sr.Color = OtherColor
			continue
		}
		col, ok := c.byKey[sr.Key]
		if !ok {
			col = Palette[c.used%len(Palette)]
			c.used++
			c.byKey[sr.Key] = col
		}
		sr.Color = col
	}
	return out
}
