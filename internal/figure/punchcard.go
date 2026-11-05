package figure

import (
	"fmt"
	"math"

	"github.com/seletz/total-recall/internal/canvas"
)

// PunchcardData is the input of Punchcard: the own messages and the agent
// events per weekday (0 = Monday) and local hour.
type PunchcardData struct {
	Human, Agent [7][24]int
}

// Punchcard lays out weekday × hour: one row per weekday with a circle per
// hour, a large translucent one for the agent activity and a solid one on
// top for the own messages, each scaled by the square root of its share of
// its own maximum, the hours below and a legend. About 760×290.
func Punchcard(d PunchcardData, o Options) canvas.Scene {
	const cw, rh, left = 30.0, 30.0, 30.0
	s := canvas.Scene{Background: background(o)}
	tx := o.Lang.text()
	top := heading(&s, o, tx.punchcardTitle) + 18
	mh, ma := 1, 1
	for i := range 7 {
		for h := range 24 {
			mh = max(mh, d.Human[i][h])
			ma = max(ma, d.Agent[i][h])
		}
	}
	for i, wd := range tx.weekdays {
		y := top + float64(i)*rh
		s.Add(label(0, y+4, wd[:2], canvas.Start))
		s.Add(canvas.Line{X1: left, Y1: y, X2: left + 24*cw, Y2: y, Stroke: EmptyColor})
		for h := range 24 {
			cx := left + float64(h)*cw + cw/2
			if a := d.Agent[i][h]; a > 0 {
				s.Add(canvas.Circle{CX: cx, CY: y, R: 2 + 12*math.Sqrt(float64(a)/float64(ma)), Fill: canvas.Alpha(AgentColor, 0.45),
					Title: fmt.Sprintf("%s %02d–%02d: %d %s", wd, h, h+1, a, tx.agentEvents)})
			}
			if v := d.Human[i][h]; v > 0 {
				s.Add(canvas.Circle{CX: cx, CY: y, R: 1.5 + 8*math.Sqrt(float64(v)/float64(mh)), Fill: HumanColor,
					Title: fmt.Sprintf("%s %02d–%02d: %d %s", wd, h, h+1, v, tx.ownMessages)})
			}
		}
	}
	y := top + 7*rh - 10
	for h := range 24 {
		s.Add(label(left+float64(h)*cw+cw/2, y, itoa(h), canvas.Middle))
	}
	s.Width = left + 24*cw + 10
	s.Height = y + 8
	if !o.NoLegend {
		s.Add(canvas.Circle{CX: left + 6, CY: y + 20, R: 6, Fill: HumanColor})
		s.Add(label(left+16, y+24, tx.ownMessages, canvas.Start))
		s.Add(canvas.Circle{CX: left + 176, CY: y + 20, R: 6, Fill: canvas.Alpha(AgentColor, 0.45)})
		s.Add(label(left+186, y+24, tx.agentActivity, canvas.Start))
		s.Height = y + 32
	}
	return s
}
