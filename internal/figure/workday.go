package figure

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"time"

	"github.com/seletz/total-recall/internal/canvas"
)

// WorkdayRow is one day of Workday. Positions are hours from the day
// boundary, where the time axis starts.
type WorkdayRow struct {
	// Day is the calendar day (any time of it); it gives the weekday and
	// the date of the label.
	Day time.Time
	// Active is true for a day with activity; First and Last are then
	// the positions of the first and the last activity, FirstText and
	// LastText their clock times ("08:12", "01:30 (+1)").
	Active      bool
	First, Last float64
	FirstText   string
	LastText    string
	// Span is the time between first and last activity, Diff its
	// difference from the target (HasDiff = false: nothing to compare).
	Span, Diff time.Duration
	HasDiff    bool
	// Weekend marks a Saturday or Sunday, Holiday names a public
	// holiday ("" on an ordinary day).
	Weekend bool
	Holiday string
	// HasCore says CoreStart and CoreEnd, the core hours, are drawn as a
	// band; Target is the daily target in hours, drawn as a line from the
	// first activity (0 = none).
	HasCore            bool
	CoreStart, CoreEnd float64
	Target             float64
}

// WorkdayData is the input of Workday: one row per day of the range and
// the sums of the period.
type WorkdayData struct {
	// DayStart is the wall-clock hour of the axis start (4 = 04:00);
	// Midnight is the position of the next midnight on the axis (20 for a
	// day that starts at 04:00): activity after it is the overflow.
	DayStart, Midnight float64
	Rows               []WorkdayRow
	// Span is the working time of the period in Active days; Target the
	// sum of the targets (Workdays × DayTarget) and Balance Span minus
	// Target. HasTarget is false without configured working time: the
	// headline then shows the span only.
	Span, Target, Balance time.Duration
	Active, Workdays      int
	DayTarget             time.Duration
	HasTarget             bool
}

// Workday lays out the working days like the activity view of the TUI: one
// row per day with the weekday and date, a bar from the first to the last
// activity on a time axis that starts at the day boundary, and the first
// and the last activity, the span and its difference from the target as
// text. The core hours are a band behind the bar, the target a dashed line
// under it; the bar is blue within the core hours and orange before and
// after them, a darker orange after midnight; a weekend is grey, a public
// holiday red with its name on the axis. The headline sums the period.
// Every bar carries its times as a tooltip. About 720×(60 + 17 per day).
func Workday(d WorkdayData, o Options) canvas.Scene {
	// The columns right of the axis: widths of first, last, span, difference.
	const hourW, rowH, barH = 17.0, 17.0, 9.0
	const firstW, lastW, spanW, diffW = 36.0, 64.0, 50.0, 56.0
	th := o.theme()
	tx := o.Lang.text()
	s := canvas.Scene{Background: th.Background}
	x0 := 66.0
	x1 := x0 + 24*hourW
	top := heading(&s, o, tx.workdayTitle)
	if line := headline(d, o.Lang); line != "" {
		s.Add(canvas.Text{X: 0, Y: top + 12, Text: line, Size: labelSize, Fill: th.Text})
		top += 20
	}
	xOf := func(h float64) float64 { return x0 + max(0, min(h, 24))*hourW }

	core, outside, night := step(th.Blue, 2), step(th.Orange, 1), step(th.Orange, 2)
	weekend, holiday := canvas.Alpha(th.Text, 0.55), canvas.Hex(holidayHex)

	// The axis: an hour label every two hours with a grid line below it.
	axisY := top + 10
	first := int(math.Ceil(d.DayStart))
	var ticks []float64
	for h := first; float64(h) < d.DayStart+24; h++ {
		if h%2 != 0 {
			continue
		}
		x := xOf(float64(h) - d.DayStart)
		ticks = append(ticks, x)
		s.Add(canvas.Text{X: x, Y: axisY, Text: itoa(h % 24), Size: smallSize, Fill: th.Text, Anchor: canvas.Middle})
	}
	top += 16
	rows := float64(len(d.Rows)) * rowH
	for _, x := range ticks {
		s.Add(canvas.Line{X1: x, Y1: top, X2: x, Y2: top + rows, Stroke: gridColor})
	}
	if d.Midnight > 0 && d.Midnight < 24 {
		x := xOf(d.Midnight)
		s.Add(canvas.Line{X1: x, Y1: top, X2: x, Y2: top + rows, Stroke: th.Text, Width: 1, Dash: [2]float64{3, 3}})
	}

	for i, r := range d.Rows {
		y := top + float64(i)*rowH
		by := y + (rowH-barH)/2
		off := r.Weekend || r.Holiday != ""
		switch {
		case r.Holiday != "":
			s.Add(canvas.Rect{X: x0, Y: y + 1, W: x1 - x0, H: rowH - 2, Radius: 2, Fill: canvas.Alpha(holiday, 0.12)})
		case r.Weekend:
			s.Add(canvas.Rect{X: x0, Y: y + 1, W: x1 - x0, H: rowH - 2, Radius: 2, Fill: canvas.Alpha(th.Text, 0.1)})
		case r.HasCore:
			s.Add(canvas.Rect{X: xOf(r.CoreStart), Y: y + 1, W: xOf(r.CoreEnd) - xOf(r.CoreStart), H: rowH - 2, Radius: 2, Fill: canvas.Alpha(core, 0.16)})
		}
		s.Add(canvas.Text{X: 0, Y: y + 12, Text: o.Lang.shortDay(r.Day), Size: labelSize, Fill: th.Text})
		if !r.Active {
			if r.Holiday != "" {
				s.Add(canvas.Text{X: x0 + 6, Y: y + 12, Text: r.Holiday, Size: smallSize, Fill: holiday})
			}
			continue
		}
		tip := fmt.Sprintf("%s: %s – %s, %s", o.Lang.shortDay(r.Day), r.FirstText, r.LastText, spanText(r.Span))
		if r.HasDiff {
			tip += ", " + diffText(r.Diff)
		}
		if r.Holiday != "" {
			tip += " (" + r.Holiday + ")"
		}
		// The bar in segments, cut at the core hours and at midnight.
		cuts := []float64{r.First, r.Last}
		if !off && d.Midnight > 0 {
			cuts = append(cuts, d.Midnight)
		}
		if !off && r.HasCore {
			cuts = append(cuts, r.CoreStart, r.CoreEnd)
		}
		cuts = within(cuts, r.First, r.Last)
		if len(cuts) < 2 { // first == last: a single tick
			s.Add(canvas.Rect{X: xOf(r.First) - 1, Y: by, W: 2, H: barH, Radius: 1, Fill: core, Title: tip})
		}
		for j := 0; j+1 < len(cuts); j++ {
			a, b := cuts[j], cuts[j+1]
			mid := (a + b) / 2
			fill := core
			switch {
			case r.Holiday != "":
				fill = holiday
			case r.Weekend:
				fill = weekend
			case d.Midnight > 0 && mid >= d.Midnight:
				fill = night
			case r.HasCore && (mid < r.CoreStart || mid >= r.CoreEnd):
				fill = outside
			}
			w := math.Max(xOf(b)-xOf(a), 2)
			s.Add(canvas.Rect{X: xOf(a), Y: by, W: w, H: barH, Radius: 2, Fill: fill, Title: tip})
		}
		if !off && r.Target > 0 {
			ty := y + rowH - 2
			s.Add(canvas.Line{X1: xOf(r.First), Y1: ty, X2: xOf(r.First + r.Target), Y2: ty, Width: 1.5, Stroke: th.Text, Dash: [2]float64{3, 2}})
		}
		if r.Holiday != "" {
			// The name beside the bar, where there is room.
			x, anchor := xOf(r.Last)+6, canvas.Start
			if x+float64(len([]rune(r.Holiday)))*5.5 > x1 {
				x, anchor = xOf(r.First)-6, canvas.End
			}
			s.Add(canvas.Text{X: x, Y: y + 12, Text: r.Holiday, Size: smallSize, Fill: holiday, Anchor: anchor})
		}
		// The columns right of the axis: first, last, span, difference.
		cx := x1 + 10 + firstW
		s.Add(value(th, cx, y+12, r.FirstText, canvas.End))
		cx += lastW
		s.Add(value(th, cx, y+12, r.LastText, canvas.End))
		cx += spanW
		s.Add(value(th, cx, y+12, spanText(r.Span), canvas.End))
		if r.HasDiff {
			cx += diffW
			s.Add(value(th, cx, y+12, diffText(r.Diff), canvas.End))
		}
	}
	s.Width = x1 + 10 + firstW + lastW + spanW + diffW
	s.Height = top + rows + 6
	if !o.NoLegend {
		y := s.Height + 8
		x := 0.0
		for _, l := range []struct {
			text string
			c    color.NRGBA
		}{{tx.coreHours, core}, {tx.outsideCore, outside}, {tx.afterMidnight, night}, {tx.weekend, weekend}, {tx.holiday, holiday}} {
			s.Add(canvas.Rect{X: x, Y: y, W: 12, H: 9, Radius: 2, Fill: l.c})
			s.Add(label(th, x+17, y+9, l.text, canvas.Start))
			x += 17 + 6.2*float64(len([]rune(l.text))) + 14
		}
		s.Add(canvas.Line{X1: x, Y1: y + 5, X2: x + 14, Y2: y + 5, Width: 1.5, Stroke: th.Text, Dash: [2]float64{3, 2}})
		s.Add(label(th, x+19, y+9, tx.targetSpan, canvas.Start))
		x += 19 + 6.2*float64(len([]rune(tx.targetSpan)))
		s.Width = max(s.Width, x)
		s.Height = y + 14
	}
	return s
}

// headline sums the period: "142h30m in 19 days · target 160h00m (20
// workdays × 8h00m) · balance -17h30m"; without a target only the span.
func headline(d WorkdayData, l Lang) string {
	tx := l.text()
	days := l.count(d.Active, tx.dayOne, tx.dayMany)
	if !d.HasTarget {
		return fmt.Sprintf("%s (%s)", spanText(d.Span), days)
	}
	return fmt.Sprintf(tx.headline, spanText(d.Span), days, spanText(d.Target),
		l.count(d.Workdays, tx.workdayOne, tx.workdayMany), spanText(d.DayTarget), diffText(d.Balance))
}

// within returns the distinct positions of cuts between lo and hi, in
// order.
func within(cuts []float64, lo, hi float64) []float64 {
	var in []float64
	for _, c := range cuts {
		if c >= lo && c <= hi {
			in = append(in, c)
		}
	}
	sort.Float64s(in)
	out := in[:0]
	for _, c := range in {
		if len(out) == 0 || c != out[len(out)-1] {
			out = append(out, c)
		}
	}
	return out
}
