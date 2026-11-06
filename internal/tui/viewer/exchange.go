package viewer

import (
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/session"
)

// Exchange shows an event in its exchange: the session head and the
// events around the hit, each with a separator (time, actor), the hit's
// separator marked. It is the viewer of an event hit.
type Exchange struct {
	Session store.SessionInfo
	// Events are the events of the exchange, Hit the index of the hit
	// among them.
	Events []model.Event
	Hit    int
}

// Render implements Viewer.
func (v Exchange) Render(env Env, w int) Content {
	c := Content{Head: sessionHead(env, v.Session, w)}
	for i, e := range v.Events {
		if i == v.Hit {
			c.Anchor = len(c.Body)
		}
		c.Body = append(c.Body, eventLines(env, e, i == v.Hit, w)...)
		if i < len(v.Events)-1 {
			c.Body = append(c.Body, "")
		}
	}
	return c
}

// eventLines renders one event: its separator (time muted, the actor in
// its colour, the hit marked) and the text wrapped to w with the terms
// highlighted.
func eventLines(env Env, e model.Event, hit bool, w int) []string {
	sep := session.Separator(env.Theme, e.TS.In(env.loc()).Format("15:04"), e, w)
	if hit {
		sep += env.Theme.Mark.Render(MarkText)
	}
	return append([]string{sep}, textLines(env, e.Text, w, lipgloss.NewStyle())...)
}
