package viewer

import (
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/session"
)

// Tool shows a tool call: the session head, then the input and the output
// under their own separators, JSON indented. It is the viewer of an event
// hit of kind tool_use or tool_result; the hit's separator is marked.
type Tool struct {
	Session store.SessionInfo
	// Use is the call, Result its output; either may be nil when the
	// archive does not hold the counterpart.
	Use, Result *model.Event
	// HitResult tells that the hit is the result, not the call.
	HitResult bool
}

// Render implements Viewer.
func (v Tool) Render(env Env, w int) Content {
	c := Content{Head: sessionHead(env, v.Session, w)}
	section := func(e *model.Event, none string, hit bool) {
		if e == nil {
			c.Body = append(c.Body, env.Theme.Muted.Render("── "+none))
			return
		}
		sep := session.Separator(env.Theme, e.TS.In(env.loc()).Format("15:04:05"), *e, w)
		if hit {
			c.Anchor = len(c.Body)
			sep += env.Theme.Mark.Render(MarkText)
		}
		c.Body = append(c.Body, sep)
		text, _ := prettyJSON(e.Text)
		c.Body = append(c.Body, textLines(env, text, w, lipgloss.NewStyle())...)
	}
	section(v.Use, "no call recorded", !v.HitResult)
	c.Body = append(c.Body, "")
	section(v.Result, "no result recorded", v.HitResult)
	return c
}

// PairTool finds the call and the result that belong together around the
// event at index i of events, which must be a tool_use or tool_result:
// the result of a call is the next tool result after it, the call of a
// result the closest tool call before it (the archive keeps no explicit
// link, and Claude Code writes the result right after its call).
func PairTool(events []model.Event, i int) (use, result *model.Event) {
	switch events[i].Kind {
	case model.KindToolUse:
		use = &events[i]
		for j := i + 1; j < len(events); j++ {
			if events[j].Kind == model.KindToolResult {
				result = &events[j]
				break
			}
			if events[j].Kind == model.KindToolUse {
				break
			}
		}
	case model.KindToolResult:
		result = &events[i]
		for j := i - 1; j >= 0; j-- {
			if events[j].Kind == model.KindToolUse {
				use = &events[j]
				break
			}
			if events[j].Kind == model.KindToolResult {
				break
			}
		}
	}
	return use, result
}
