package figure

import (
	"fmt"
	"image/color"

	"github.com/seletz/total-recall/internal/canvas"
)

// ProjectRow is one project of Projects: its name, its agent and human
// events over the range and its colour.
type ProjectRow struct {
	Name         string
	Human, Agent int
	Color        color.NRGBA
}

// Projects lays out the share per project: one row per project, top first,
// with two horizontal bars, the agent work (agent events, the project
// colour) and the own attention (own messages, the same colour lighter),
// each as a share of all events of its kind, the longest bar filling the
// width. About 610×370 for twelve rows.
func Projects(rows []ProjectRow, o Options) canvas.Scene {
	const left, bar, rowH, plotW, right = 170.0, 9.0, 26.0, 380.0, 60.0
	th := o.theme()
	s := canvas.Scene{Background: th.Background}
	tx := o.Lang.text()
	top := heading(&s, o, tx.projectsTitle)
	if !o.NoLegend {
		swatch := th.Text
		for i, l := range []struct {
			text string
			fill color.NRGBA
		}{{tx.agentWork, swatch}, {tx.ownAttention, canvas.Alpha(swatch, 0.5)}} {
			x := left + float64(i)*200
			s.Add(canvas.Rect{X: x, Y: top + 2, W: 12, H: 10, Radius: 2, Fill: l.fill})
			s.Add(label(th, x+18, top+11, l.text, canvas.Start))
		}
		top += 18
	}
	top += 2
	sumH, sumA := 0, 0
	for _, r := range rows {
		sumH += r.Human
		sumA += r.Agent
	}
	share := func(v, sum int) float64 {
		if sum == 0 {
			return 0
		}
		return float64(v) / float64(sum)
	}
	mx := 0.0
	for _, r := range rows {
		mx = max(mx, share(r.Agent, sumA), share(r.Human, sumH))
	}
	if mx == 0 {
		mx = 1
	}
	for i, r := range rows {
		y := top + float64(i)*rowH
		s.Add(label(th, left-8, y+14, cut(r.Name, 26), canvas.End))
		for j, b := range []struct {
			v, sum int
			fill   color.NRGBA
			what   string
		}{{r.Agent, sumA, r.Color, tx.agentEvents}, {r.Human, sumH, canvas.Alpha(r.Color, 0.5), tx.ownMessages}} {
			w := plotW * share(b.v, b.sum) / mx
			by := y + float64(j)*(bar+1)
			s.Add(canvas.Rect{X: left, Y: by, W: w, H: bar, Radius: 2, Fill: b.fill,
				Title: fmt.Sprintf("%s: %d %s (%s)", r.Name, b.v, b.what, percent(b.v, b.sum))})
			s.Add(value(th, left+w+4, by+8, percent(b.v, b.sum), canvas.Start))
		}
	}
	s.Width = left + plotW + right
	s.Height = top + float64(len(rows))*rowH + 4
	return s
}
