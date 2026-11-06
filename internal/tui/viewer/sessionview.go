package viewer

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/session"
)

// FirstMessages is how many messages of a session the Session viewer
// shows, and LinesPerMessage how many lines of each.
const (
	FirstMessages   = 5
	LinesPerMessage = 6
)

// Session shows a session: its head, the counts and the first messages.
// It is the viewer of a session hit.
type Session struct {
	Info store.SessionInfo
	// First are the first messages of the session, oldest first.
	First []model.Event
}

// Render implements Viewer.
func (v Session) Render(env Env, w int) Content {
	c := Content{Head: sessionHead(env, v.Info, w), Anchor: -1}
	counts := fmt.Sprintf("%d events (%d human, %d agent)", v.Info.Events, v.Info.HumanEvents, v.Info.AgentEvents)
	if v.Info.Harness != "" {
		counts += "  " + v.Info.Harness
	}
	c.Body = append(c.Body, " "+env.Theme.Muted.Render(cut(counts, w)), "")
	for i, e := range v.First {
		c.Body = append(c.Body, session.Separator(env.Theme, e.TS.In(env.loc()).Format("15:04"), e, w))
		lines := textLines(env, e.Text, w, lipgloss.NewStyle())
		if len(lines) > LinesPerMessage {
			lines = append(lines[:LinesPerMessage], env.Theme.Muted.Render("…"))
		}
		c.Body = append(c.Body, lines...)
		if i < len(v.First)-1 {
			c.Body = append(c.Body, "")
		}
	}
	if len(v.First) == 0 {
		c.Body = append(c.Body, " "+env.Theme.Muted.Render("no messages"))
	}
	return c
}
