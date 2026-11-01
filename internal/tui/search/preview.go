package search

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/session"
)

// PreviewDebounce is the pause after the marked hit changed before its
// session is loaded for the preview.
const PreviewDebounce = 100 * time.Millisecond

// Loader reads a session and its events for the preview; *store.Store
// implements it.
type Loader interface {
	FindSession(ctx context.Context, ref string) (store.SessionInfo, error)
	Events(ctx context.Context, sessionID string, all bool) ([]model.Event, error)
}

// previewTickMsg fires after PreviewDebounce; it loads the marked hit when
// seq is still the current preview sequence.
type previewTickMsg struct{ seq uint64 }

// previewMsg is the loaded exchange around one hit; it is applied only when
// seq is still the current preview sequence.
type previewMsg struct {
	seq     uint64
	eventID string
	session store.SessionInfo
	events  []model.Event // the window around the hit
	hit     int           // index of the hit within events
	err     error
}

// preview is the state of the right column: the hit it shows (or failed to
// show) and the load in flight.
type preview struct {
	// seq counts changes of the marked hit; ticks and results carry the
	// value they were started for and are ignored when it moved on.
	seq     uint64
	cancel  context.CancelFunc
	loading bool

	eventID string // hit of the loaded data, empty before the first result
	session store.SessionInfo
	events  []model.Event
	hit     int
	err     error
}

// stop cancels the running load, if any.
func (p *preview) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
}

// marked returns the marked hit, nil without hits.
func (m Model) marked() *store.SearchHit {
	if len(m.hits) == 0 {
		return nil
	}
	return &m.hits[m.cursor]
}

// follow is called whenever the marked hit may have changed. It drops
// pending and running work, clears the preview when there is no hit, and
// otherwise schedules a load after PreviewDebounce unless the hit is already
// shown.
func (m *Model) follow() tea.Cmd {
	p := &m.preview
	p.seq++
	p.stop()
	h := m.marked()
	if h == nil {
		*p = preview{seq: p.seq}
		return nil
	}
	if h.Event.ID == p.eventID || m.opts.Loader == nil {
		return nil
	}
	seq := p.seq
	return tea.Tick(PreviewDebounce, func(time.Time) tea.Msg { return previewTickMsg{seq: seq} })
}

// loadPreview starts reading the session of the marked hit.
func (m Model) loadPreview() (tea.Model, tea.Cmd) {
	h := m.marked()
	if h == nil {
		return m, nil
	}
	p := &m.preview
	p.stop()
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.loading = cancel, true
	seq, ld, sessionID, eventID := p.seq, m.opts.Loader, h.Event.SessionID, h.Event.ID
	return m, func() tea.Msg {
		defer cancel()
		res := previewMsg{seq: seq, eventID: eventID}
		s, err := ld.FindSession(ctx, sessionID)
		if err != nil {
			res.err = err
			return res
		}
		res.session = s
		events, err := ld.Events(ctx, s.ID, false)
		if err != nil {
			res.err = err
			return res
		}
		start, end := window(events, eventID)
		if start < 0 {
			// Not among the messages, so probably a tool event: take the
			// full transcript.
			if events, err = ld.Events(ctx, s.ID, true); err != nil {
				res.err = err
				return res
			}
			start, end = window(events, eventID)
		}
		if start < 0 {
			res.err = errors.New("event not found")
			return res
		}
		res.events, res.hit = events[start:end+1], indexOf(events, eventID)-start
		return res
	}
}

// previewResult applies a load result.
func (m Model) previewResult(msg previewMsg) (tea.Model, tea.Cmd) {
	p := &m.preview
	if msg.seq != p.seq {
		return m, nil
	}
	p.loading, p.cancel = false, nil
	p.eventID, p.session, p.events, p.hit, p.err = msg.eventID, msg.session, msg.events, msg.hit, msg.err
	return m, nil
}

// window returns the index range [start, end] of the exchange around the
// event hitID: from the last human or peer message at or before the hit to
// the first agent message at or after it. A hit that is itself a prompt
// starts the window; one that is itself a reply ends it. Both are -1 when
// the event is not listed.
func window(events []model.Event, hitID string) (start, end int) {
	i := indexOf(events, hitID)
	if i < 0 {
		return -1, -1
	}
	start, end = i, i
	if !isPrompt(events[i]) {
		for j := i - 1; j >= 0; j-- {
			if isPrompt(events[j]) {
				start = j
				break
			}
		}
	}
	if !isReply(events[i]) {
		for j := i + 1; j < len(events); j++ {
			if isReply(events[j]) {
				end = j
				break
			}
		}
	}
	return start, end
}

func isPrompt(e model.Event) bool {
	return e.Kind == model.KindMessage && (e.Actor == model.ActorHuman || e.Actor == model.ActorPeer)
}

func isReply(e model.Event) bool {
	return e.Kind == model.KindMessage && e.Actor == model.ActorAgent
}

// indexOf returns the index of the event with id, -1 if it is not listed.
func indexOf(events []model.Event, id string) int {
	for i, e := range events {
		if e.ID == id {
			return i
		}
	}
	return -1
}

var nameStyle = lipgloss.NewStyle().Bold(true)

// markText is appended to the separator of the hit, like in the detail view.
const markText = " ◄"

// minIDWidth is the narrowest the session ID is shown at in the head.
const minIDWidth = 12

// previewView renders the right column for width w and rows lines: the
// session head, a rule, and the exchange around the marked hit with the
// search terms highlighted and the hit's separator marked.
func (m Model) previewView(w, rows int) string {
	p := m.preview
	h := m.marked()
	switch {
	case h == nil || m.opts.Loader == nil || w <= 0:
		return ""
	case p.eventID != h.Event.ID:
		return " " + faintStyle.Render("loading…")
	case p.err != nil:
		return " " + ansi.Truncate("error: "+p.err.Error(), max(w-1, 0), "…")
	}
	lines := m.previewHead(w)
	head := len(lines)
	hitLine := 0
	ms := terms(m.query, m.filters.raw)
	for i, e := range p.events {
		if i == p.hit {
			hitLine = len(lines)
		}
		lines = append(lines, m.previewEvent(e, i == p.hit, ms, w)...)
		if i < len(p.events)-1 {
			lines = append(lines, "")
		}
	}
	// Keep the hit in the upper part of the column when the exchange is
	// longer than the column; the head stays.
	body := lines[head:]
	if avail := rows - head; len(body) > avail && avail > 0 {
		off := min(max(hitLine-head-avail/3, 0), len(body)-avail)
		body = body[off:]
	}
	return strings.Join(append(lines[:head:head], body...), "\n")
}

// previewHead is the session head: name, ID and repo, time span and branch,
// then a rule.
func (m Model) previewHead(w int) []string {
	s := m.preview.session
	name := s.DisplayName()
	if name == "" {
		name = "(untitled)"
	}
	cut := func(str string) string { return ansi.Truncate(str, max(w-1, 0), "…") }
	// The repo wins over the ID, which is shortened to the room left.
	second := s.ID
	if r := session.ShortRepo(s.Repo()); r != "" {
		room := w - 1 - ansi.StringWidth(r) - 2
		second = ansi.Truncate(s.ID, max(room, minIDWidth), "…") + "  " + r
	}
	third := session.TimeSpan(s.StartedAt, s.EndedAt, m.opts.Location)
	if s.GitBranch != "" {
		third += "  " + s.GitBranch
	}
	return []string{
		" " + nameStyle.Render(cut(name)),
		" " + faintStyle.Render(cut(second)),
		" " + cut(third),
		strings.Repeat("─", w),
	}
}

// previewEvent renders one event: its separator (time and actor, the hit
// marked) and the text wrapped to w with the terms highlighted.
func (m Model) previewEvent(e model.Event, hit bool, ts []string, w int) []string {
	label := string(e.Actor)
	if e.Kind == model.KindToolUse {
		label = "tool_use " + e.ToolName
	} else if e.Kind != model.KindMessage {
		label += " (" + string(e.Kind) + ")"
	}
	sep := faintStyle.Render(ansi.Truncate("── "+e.TS.In(m.opts.Location).Format("15:04")+"  "+label, w, ""))
	if hit {
		sep += nameStyle.Render(markText)
	}
	out := []string{sep}
	text := strings.ReplaceAll(strings.TrimRight(e.Text, "\n"), "\t", "    ")
	text = strings.ReplaceAll(text, "\r", "")
	if text == "" {
		return out
	}
	lines := strings.Split(ansi.Wrap(text, w, ""), "\n")
	matches := session.Matches(lines, ts)
	next := 0
	for i, line := range lines {
		start := next
		for next < len(matches) && matches[next].Line == i {
			next++
		}
		out = append(out, highlightLine(line, matches[start:next]))
	}
	return out
}

// highlightLine styles the matched ranges of line.
func highlightLine(line string, ms []session.Match) string {
	if len(ms) == 0 {
		return line
	}
	var b strings.Builder
	pos := 0
	for _, m := range ms {
		b.WriteString(line[pos:m.Start])
		b.WriteString(hitStyle.Render(line[m.Start:m.End]))
		pos = m.End
	}
	b.WriteString(line[pos:])
	return b.String()
}
