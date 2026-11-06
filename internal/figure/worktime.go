package figure

import (
	"fmt"
	"image/color"
	"math"

	"github.com/seletz/total-recall/internal/canvas"
)

// WorktimeCategory is one category of Worktime with its colour; the
// order of the categories is the stacking order, bottom first.
type WorktimeCategory struct {
	Name  string
	Color color.NRGBA
}

// WorktimeWeek is one week of Worktime: the hours per category (keyed by
// name, missing = 0) and the target of the week in hours (daily target ×
// workdays; 0 draws no target).
type WorktimeWeek struct {
	Label  string // Lang.Week: W41, KW 41
	Hours  map[string]float64
	Target float64
}

// WorktimeData is the input of Worktime.
type WorktimeData struct {
	Categories []WorktimeCategory
	Weeks      []WorktimeWeek
}

// Worktime lays out the working time per week: one stacked bar per week,
// one segment per category, a line across every bar at the target of the
// week, a grid in hours and a legend of the categories. About 640×300
// for five weeks.
func Worktime(d WorktimeData, o Options) canvas.Scene {
	const left, colW, barW, plotH, legendW = 44.0, 64.0, 40.0, 200.0, 170.0
	th := o.theme()
	s := canvas.Scene{Background: th.Background}
	tx := o.Lang.text()
	top := heading(&s, o, tx.worktimeTitle) + 14
	mx := 0.0
	for _, w := range d.Weeks {
		sum := 0.0
		for _, c := range d.Categories {
			sum += w.Hours[c.Name]
		}
		mx = max(mx, sum, w.Target)
	}
	step := 10.0
	if mx <= 20 {
		step = 5
	}
	ymax := max(step, math.Ceil(mx/step)*step)
	yOf := func(h float64) float64 { return top + plotH - plotH*h/ymax }
	plotW := float64(max(len(d.Weeks), 1)) * colW
	for h := 0.0; h <= ymax; h += step {
		y := yOf(h)
		s.Add(canvas.Line{X1: left, Y1: y, X2: left + plotW, Y2: y, Stroke: gridColor})
		s.Add(label(th, left-6, y+4, o.Lang.hours(h), canvas.End))
	}
	for i, w := range d.Weeks {
		x := left + float64(i)*colW + (colW-barW)/2
		base := 0.0
		for _, c := range d.Categories {
			h := w.Hours[c.Name]
			if h <= 0 {
				continue
			}
			s.Add(canvas.Rect{X: x, Y: yOf(base + h), W: barW, H: yOf(base) - yOf(base+h), Fill: c.Color,
				Title: fmt.Sprintf("%s %s: %s", w.Label, c.Name, o.Lang.hours(h))})
			base += h
		}
		if w.Target > 0 {
			y := yOf(w.Target)
			s.Add(canvas.Line{X1: x - 4, Y1: y, X2: x + barW + 4, Y2: y, Width: 1.5, Stroke: th.Text, Dash: [2]float64{4, 3}})
		}
		s.Add(label(th, x+barW/2, top+plotH+16, w.Label, canvas.Middle))
		if base > 0 {
			s.Add(value(th, x+barW/2, yOf(base)-4, o.Lang.hours(base), canvas.Middle))
		}
	}
	s.Width = left + plotW + 10
	s.Height = top + plotH + 22
	if !o.NoLegend {
		lx := left + plotW + 24
		y := top + 4
		for _, c := range d.Categories {
			s.Add(canvas.Rect{X: lx, Y: y, W: 12, H: 12, Radius: 2, Fill: c.Color})
			s.Add(label(th, lx+18, y+10, cut(c.Name, 20), canvas.Start))
			y += 18
		}
		s.Add(canvas.Line{X1: lx, Y1: y + 6, X2: lx + 12, Y2: y + 6, Width: 1.5, Stroke: th.Text, Dash: [2]float64{4, 3}})
		s.Add(label(th, lx+18, y+10, tx.target, canvas.Start))
		s.Width = lx + legendW
		s.Height = max(s.Height, y+24)
	}
	return s
}
