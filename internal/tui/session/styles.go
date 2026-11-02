package session

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// LineStyles are the styles HighlightLine draws a transcript line with:
// Base for the text between the matches (the zero value leaves it plain),
// Match for a match and Selected for the selected match.
type LineStyles struct {
	Base, Match, Selected lipgloss.Style
}

// Separator renders the separator line of an event for the transcript and
// the search preview: the rule stub and the time ts muted, the label (the
// actor, tool calls with the tool name, see Label) bold in the actor's
// colour, cut to w cells. Its plain text is "── <ts>  <label>".
func Separator(th theme.Theme, ts string, ev model.Event, w int) string {
	actor := ev.Actor
	if ev.Kind == model.KindToolUse || ev.Kind == model.KindToolResult {
		actor = model.ActorTool
	}
	return ansi.Truncate(th.Muted.Render("── "+ts+"  ")+th.Actor(actor).Render(Label(ev)), w, "")
}

// Label names an event on its separator: the actor, tool calls with the
// tool name, other non-message kinds with the kind.
func Label(ev model.Event) string {
	switch {
	case ev.Kind == model.KindToolUse:
		return "tool_use " + ev.ToolName
	case ev.Kind != model.KindMessage:
		return string(ev.Actor) + " (" + string(ev.Kind) + ")"
	}
	return string(ev.Actor)
}

// textStyle is the style of an event's text lines: muted for tool calls,
// tool results and the other non-message kinds, so they stand back from
// the conversation; plain for messages.
func textStyle(th theme.Theme, ev model.Event) lipgloss.Style {
	if ev.Kind != model.KindMessage {
		return th.Muted
	}
	return lipgloss.NewStyle()
}
