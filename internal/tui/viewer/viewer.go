// Package viewer renders one object of the archive for the preview column
// of the search view: an exchange of a session, a session, a memory file,
// a tool call with its result. Every kind of object has a Viewer; the
// search view picks one by the type of the marked hit and asks it for its
// lines, so a new type (an image, a diff) joins by implementing Viewer.
//
// A viewer is a plain value over data the caller has loaded: it does no
// I/O, and Render is a pure function of the data, the environment and the
// width, so a viewer is tested without a terminal.
package viewer

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/session"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// Env is what every viewer renders with.
type Env struct {
	Theme theme.Theme
	// Location is the time zone times are shown in; nil means UTC.
	Location *time.Location
	// Terms are the words of the search, highlighted in the text.
	Terms []string
	// Rows is the height of the column the viewer is drawn in; only the
	// viewers that fit their content to it (an image) look at it, 0 means
	// unknown.
	Rows int
}

func (e Env) loc() *time.Location {
	if e.Location == nil {
		return time.UTC
	}
	return e.Location
}

// Content is the rendered object: Head stays on top, Body scrolls. Anchor
// is the line of Body the view keeps in sight when it has not been
// scrolled (the hit), -1 for none.
type Content struct {
	Head, Body []string
	Anchor     int
}

// Viewer renders one object for width w.
type Viewer interface {
	Render(env Env, w int) Content
}

// MarkText is appended to the separator of the hit, like in the detail
// view.
const MarkText = " ◄"

// minIDWidth is the narrowest the session ID is shown at in the head.
const minIDWidth = 12

func cut(s string, w int) string { return ansi.Truncate(s, max(w-1, 0), "…") }

func rule(env Env, w int) string { return env.Theme.Rule.Render(strings.Repeat("─", max(w, 0))) }

// sessionHead is the session head of the viewers: the name in the title
// style, ID and repo muted, time span and branch, then a rule.
func sessionHead(env Env, s store.SessionInfo, w int) []string {
	name := s.DisplayName()
	if name == "" {
		name = "(untitled)"
	}
	// The repo wins over the ID, which is shortened to the room left.
	second := s.ID
	if r := session.ShortRepo(s.Repo()); r != "" {
		room := w - 1 - ansi.StringWidth(r) - 2
		second = ansi.Truncate(s.ID, max(room, minIDWidth), "…") + "  " + r
	}
	third := session.TimeSpan(s.StartedAt, s.EndedAt, env.loc())
	if s.GitBranch != "" {
		third += "  " + s.GitBranch
	}
	return []string{
		" " + env.Theme.Title.Render(cut(name, w)),
		" " + env.Theme.Muted.Render(cut(second, w)),
		" " + cut(third, w),
		rule(env, w),
	}
}

// textLines wraps text to w and renders it with the search terms
// highlighted over the style base. Tabs and carriage returns are
// normalised first.
func textLines(env Env, text string, w int, base lipgloss.Style) []string {
	text = strings.ReplaceAll(strings.TrimRight(text, "\n"), "\t", "    ")
	text = strings.ReplaceAll(text, "\r", "")
	if text == "" {
		return nil
	}
	return highlight(env, strings.Split(ansi.Wrap(text, max(w, 1), ""), "\n"), base)
}

// highlight styles plain lines: base for the text, the match style for
// the occurrences of the terms.
func highlight(env Env, lines []string, base lipgloss.Style) []string {
	matches := session.Matches(lines, env.Terms)
	out := make([]string, len(lines))
	next := 0
	for i, line := range lines {
		start := next
		for next < len(matches) && matches[next].Line == i {
			next++
		}
		out[i] = session.HighlightLine(line, matches[start:next], -1, session.LineStyles{Base: base, Match: env.Theme.Match})
	}
	return out
}

// prettyJSON indents s (two spaces, key order kept) when it is one JSON
// object or array; ok is false for anything else, also for a value cut
// off by the archive's size cap.
func prettyJSON(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" || t[0] != '{' && t[0] != '[' || !json.Valid([]byte(t)) {
		return s, false
	}
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(t), "", "  "); err != nil {
		return s, false
	}
	return b.String(), true
}
