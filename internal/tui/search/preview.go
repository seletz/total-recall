package search

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/viewer"
)

// PreviewDebounce is the pause after the marked hit changed before its
// object is loaded for the preview.
const PreviewDebounce = 100 * time.Millisecond

// Loader reads what the viewers show; *store.Store implements it.
type Loader interface {
	FindSession(ctx context.Context, ref string) (store.SessionInfo, error)
	Events(ctx context.Context, sessionID string, all bool) ([]model.Event, error)
	MemoryContent(ctx context.Context, projectID int64, file string, at time.Time) (store.MemoryVersion, string, error)
}

// previewTickMsg fires after PreviewDebounce; it loads the marked hit when
// seq is still the current preview sequence.
type previewTickMsg struct{ seq uint64 }

// previewMsg is the loaded viewer of one hit; it is applied only when seq
// is still the current preview sequence.
type previewMsg struct {
	seq  uint64
	key  string
	view viewer.Viewer
	err  error
}

// preview is the state of the right column: the hit it shows (or failed to
// show) and the load in flight.
type preview struct {
	// seq counts changes of the marked hit; ticks and results carry the
	// value they were started for and are ignored when it moved on.
	seq     uint64
	cancel  context.CancelFunc
	loading bool

	key  string // hit of the loaded data (see hitKey), empty before the first result
	view viewer.Viewer
	err  error
	// scroll is the first line of the body shown once the mouse wheel has
	// moved it (scrolled); before that the hit is kept in view.
	scrolled bool
	scroll   int
}

// stop cancels the running load, if any.
func (p *preview) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
}

// hitKey identifies a hit across searches: the event, the session or the
// memory file (of its project) it stands for.
func hitKey(h store.Hit) string {
	switch {
	case h.Event != nil:
		return "event:" + h.Event.Event.ID
	case h.Session != nil:
		return "session:" + h.Session.ID
	case h.Memory != nil:
		return "memory:" + h.Memory.Harness + ":" + h.Memory.ProjectKey + ":" + h.Memory.Path
	}
	return ""
}

// marked returns the marked hit, nil without hits.
func (m Model) marked() *store.Hit {
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
	p.scrolled, p.scroll = false, 0
	if h == nil {
		*p = preview{seq: p.seq}
		return nil
	}
	if hitKey(*h) == p.key || m.opts.Loader == nil {
		return nil
	}
	seq := p.seq
	return tea.Tick(m.opts.PreviewDebounce, func(time.Time) tea.Msg { return previewTickMsg{seq: seq} })
}

// refreshPreview loads the marked hit again without moving the exchange:
// after a reload the hit is the same, but its session may have grown. The
// scroll position set with the wheel stays.
func (m *Model) refreshPreview() tea.Cmd {
	p := &m.preview
	p.seq++
	p.stop()
	if m.marked() == nil || m.opts.Loader == nil {
		return nil
	}
	seq := p.seq
	return tea.Tick(m.opts.PreviewDebounce, func(time.Time) tea.Msg { return previewTickMsg{seq: seq} })
}

// loadPreview starts loading the viewer of the marked hit.
func (m Model) loadPreview() (tea.Model, tea.Cmd) {
	h := m.marked()
	if h == nil {
		return m, nil
	}
	p := &m.preview
	p.stop()
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.loading = cancel, true
	seq, ld, hit, project := p.seq, m.opts.Loader, *h, m.projectName(*h)
	return m, func() tea.Msg {
		defer cancel()
		res := previewMsg{seq: seq, key: hitKey(hit)}
		res.view, res.err = loadViewer(ctx, ld, hit, project)
		return res
	}
}

// loadViewer reads what the viewer of hit needs, and picks the viewer by
// the type of the hit: a tool call shows input and output, a message its
// exchange, a session its first messages, a memory file its content under
// the name of its project.
func loadViewer(ctx context.Context, ld Loader, hit store.Hit, project string) (viewer.Viewer, error) {
	switch {
	case hit.Event != nil:
		return loadEvent(ctx, ld, hit.Event.Event)
	case hit.Session != nil:
		events, err := ld.Events(ctx, hit.Session.ID, false)
		if err != nil {
			return nil, err
		}
		return viewer.Session{Info: *hit.Session, First: events[:min(len(events), viewer.FirstMessages)]}, nil
	case hit.Memory != nil:
		v, content, err := ld.MemoryContent(ctx, hit.Memory.ProjectID, hit.Memory.Path, time.Time{})
		if err != nil {
			return nil, err
		}
		return viewer.Memory{Path: hit.Memory.Path, Project: project, Harness: hit.Memory.Harness, Changed: v.TS, Content: content}, nil
	}
	return nil, errors.New("no viewer for this hit")
}

// memoryProject names the project of a memory hit without a catalog: the
// base name of its raw key, else the harness's native key.
func memoryProject(h store.Hit) string {
	if h.Project.Key != "" {
		return h.Project.Key
	}
	return h.Memory.ProjectKey
}

// loadEvent reads the session of an event hit and picks the viewer: the
// exchange around a message, call and output of a tool event.
func loadEvent(ctx context.Context, ld Loader, e model.Event) (viewer.Viewer, error) {
	s, err := ld.FindSession(ctx, e.SessionID)
	if err != nil {
		return nil, err
	}
	var events []model.Event
	if e.Kind == model.KindMessage {
		if events, err = ld.Events(ctx, s.ID, false); err != nil {
			return nil, err
		}
	}
	i := indexOf(events, e.ID)
	if i < 0 {
		// Not among the messages, so probably a tool event: take the
		// full transcript.
		if events, err = ld.Events(ctx, s.ID, true); err != nil {
			return nil, err
		}
		if i = indexOf(events, e.ID); i < 0 {
			return nil, errors.New("event not found")
		}
	}
	if k := events[i].Kind; k == model.KindToolUse || k == model.KindToolResult {
		use, result := viewer.PairTool(events, i)
		return viewer.Tool{Session: s, Use: use, Result: result, HitResult: k == model.KindToolResult}, nil
	}
	start, end := window(events, e.ID)
	return viewer.Exchange{Session: s, Events: events[start : end+1], Hit: i - start}, nil
}

// previewResult applies a load result.
func (m Model) previewResult(msg previewMsg) (tea.Model, tea.Cmd) {
	p := &m.preview
	if msg.seq != p.seq {
		return m, nil
	}
	p.loading, p.cancel = false, nil
	if msg.key != p.key {
		// A new hit starts at the automatic position; the same hit loaded
		// again (reload) keeps the position set with the wheel.
		p.scrolled, p.scroll = false, 0
	}
	p.key, p.view, p.err = msg.key, msg.view, msg.err
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

// viewEnv is the environment the viewers render with.
func (m Model) viewEnv() viewer.Env {
	return viewer.Env{Theme: m.th, Location: m.opts.Location, Terms: terms(m.query, m.filters.raw)}
}

// previewView renders the right column for width w and rows lines: the
// head of the viewer and as much of its body as fits, from the anchor (the
// hit) unless the wheel has scrolled it.
func (m Model) previewView(w, rows int) string {
	p := m.preview
	h := m.marked()
	switch {
	case h == nil || m.opts.Loader == nil || w <= 0:
		return ""
	case p.key != hitKey(*h):
		return " " + m.th.Muted.Render("loading…")
	case p.err != nil:
		return " " + m.th.Error.Render(ansi.Truncate("error: "+p.err.Error(), max(w-1, 0), "…"))
	}
	c := p.view.Render(m.viewEnv(), w)
	if avail := rows - len(c.Head); len(c.Body) > avail && avail > 0 {
		off, limit := previewOffset(c.Anchor, len(c.Body), avail)
		if p.scrolled {
			off = min(p.scroll, limit)
		}
		c.Body = c.Body[off:]
	}
	return strings.Join(append(c.Head, c.Body...), "\n")
}

// previewOffset returns the automatic first line of a body of n lines in
// avail rows, which keeps the anchor line in the upper part of the
// column, and the largest offset that still fills the column. Without an
// anchor (< 0) the body starts at the top.
func previewOffset(anchor, n, avail int) (off, limit int) {
	limit = max(n-avail, 0)
	return min(max(anchor-avail/3, 0), limit), limit
}

// scrollPreview moves the preview by delta lines from where it is shown,
// within the body; the first line never leaves the column.
func (m *Model) scrollPreview(delta int) {
	p := &m.preview
	h := m.marked()
	if h == nil || p.key != hitKey(*h) || p.err != nil || p.view == nil {
		return
	}
	_, w := m.columns()
	rows := max(m.height-chromeRows, 0)
	c := p.view.Render(m.viewEnv(), w)
	avail := rows - len(c.Head)
	if avail <= 0 || len(c.Body) <= avail {
		return
	}
	off, limit := previewOffset(c.Anchor, len(c.Body), avail)
	if p.scrolled {
		off = min(p.scroll, limit)
	}
	p.scrolled, p.scroll = true, min(max(off+delta, 0), limit)
}
