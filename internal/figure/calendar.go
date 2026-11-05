package figure

import (
	"fmt"
	"image/color"
	"time"

	"github.com/seletz/total-recall/internal/canvas"
)

// CalendarData is the input of Calendar: the own messages per local day.
type CalendarData struct {
	// First and Last are the first and the last day shown (local
	// midnights, inclusive); every day between them is drawn.
	First, Last time.Time
	// Values are the counts per day, keyed 2006-01-02; missing days are 0.
	Values map[string]int
}

// Level buckets v into 0 … n relative to mx, the largest value: 0 is empty,
// the others split the range like the quartiles of a GitHub calendar.
func Level(v, mx, n int) int {
	if v <= 0 || mx <= 0 || n <= 0 {
		return 0
	}
	return min(n, 1+n*v/(mx+1))
}

// Calendar lays out the heatmap: ISO weeks as columns, weekdays as rows,
// one cell per day shaded by its level of scale (light to dark), the day
// number in every cell, the week numbers above and a legend with the
// maximum, the scale and the total on the right. About 360×290 for a
// month with five weeks.
func Calendar(d CalendarData, scale []color.NRGBA, o Options) canvas.Scene {
	const cell, gap, left = 32.0, 4.0, 28.0
	if len(scale) == 0 {
		scale, _ = Colors(DefaultHeatmap)
	}
	s := canvas.Scene{Background: background(o)}
	tx := o.Lang.text()
	top := heading(&s, o, tx.calendarTitle) + 12
	// The weeks: Monday of the first day's week to the last day.
	first := d.First
	last := d.Last
	if last.Before(first) {
		last = first
	}
	monday := first.AddDate(0, 0, -(int(first.Weekday())+6)%7)
	weeks := 0
	for t := monday; !t.After(last); t = t.AddDate(0, 0, 7) {
		weeks++
	}
	mx, total := 0, 0
	for t := first; !t.After(last); t = t.AddDate(0, 0, 1) {
		v := d.Values[t.Format("2006-01-02")]
		total += v
		mx = max(mx, v)
	}
	for i, wd := range tx.weekdays {
		s.Add(canvas.Text{X: 0, Y: top + float64(i)*(cell+gap) + cell/2 + 4, Text: wd[:2], Size: smallSize, Fill: TextColor})
	}
	for w := range weeks {
		t := monday.AddDate(0, 0, 7*w)
		_, wk := t.ISOWeek()
		s.Add(canvas.Text{X: left + float64(w)*(cell+gap), Y: top - 6, Text: o.Lang.Week(wk), Size: smallSize, Fill: TextColor})
	}
	for t := first; !t.After(last); t = t.AddDate(0, 0, 1) {
		w := daysBetween(monday, t) / 7
		row := (int(t.Weekday()) + 6) % 7
		v := d.Values[t.Format("2006-01-02")]
		lv := Level(v, mx, len(scale))
		fill := EmptyColor
		if lv > 0 {
			fill = scale[lv-1]
		}
		x, y := left+float64(w)*(cell+gap), top+float64(row)*(cell+gap)
		s.Add(canvas.Rect{X: x, Y: y, W: cell, H: cell, Radius: 4, Fill: fill,
			Title: fmt.Sprintf("%s %s: %d", tx.weekdays[row], o.Lang.day(t), v)})
		num := TextColor
		if lv*2 > len(scale) {
			num = Background
		}
		s.Add(canvas.Text{X: x + cell/2, Y: y + cell/2 + 4, Text: itoa(t.Day()), Size: smallSize, Fill: num, Anchor: canvas.Middle})
	}
	gridW := left + float64(weeks)*(cell+gap)
	s.Width = gridW - gap
	s.Height = top + 7*(cell+gap) - gap + 8
	if !o.NoLegend {
		lx := gridW + 12
		s.Add(label(lx, top+12, fmt.Sprintf(tx.maxPerDay, mx), canvas.Start))
		sw := append([]color.NRGBA{EmptyColor}, scale...)
		for i, c := range sw {
			s.Add(canvas.Rect{X: lx + float64(i)*18, Y: top + 22, W: 14, H: 14, Radius: 3, Fill: c})
		}
		s.Add(label(lx, top+54, fmt.Sprintf(tx.total, total), canvas.Start))
		s.Width = lx + max(float64(len(sw))*18+20, 140)
	}
	return s
}

// daysBetween counts the calendar days from a to b (both local midnights).
func daysBetween(a, b time.Time) int {
	return int(b.Sub(a).Round(24*time.Hour).Hours() / 24)
}
