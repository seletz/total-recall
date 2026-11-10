package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/theme"
)

// Open asks the view to show a session. SessionID is anything
// store.FindSession accepts (full ID, native ID or unique prefix). EventID,
// if set, is scrolled to and marked. Terms are highlighted; n/N jump between
// them.
type Open struct {
	SessionID string
	EventID   string
	Terms     []string
}

// OpenAttachments is sent by the view when the user asks for the
// attachments of the session (the attachment view is above this package,
// so the app turns it into that view's Open): EventID is the event whose
// attachment is marked first, Title the name of the session.
type OpenAttachments struct {
	SessionID string
	EventID   string
	Title     string
}

// Back is sent by the view when the user presses esc; the app returns to
// the previous view.
type Back struct{}

// loadedMsg carries the result of a load. Results of an older request
// (lower seq) are discarded.
type loadedMsg struct {
	seq     int
	session store.SessionInfo
	events  []model.Event
	all     bool
	err     error
	// atts are the attachments of the session in transcript order; a
	// failed lookup only leaves the marks out.
	atts []store.SessionAttachment
}

// chromeHeight is the number of chrome rows inside the body: two header
// lines and the status rule; a session with a continuation link has a
// third header line (see Model.chrome).
const chromeHeight = 3

// Model is the session view.
type Model struct {
	st   *store.Store
	loc  *time.Location
	keys KeyMap
	vp   viewport.Model
	// th is the theme; dark until the terminal answers the app's
	// background colour query.
	th theme.Theme

	width, height int

	seq     int
	cancel  context.CancelFunc
	open    Open
	all     bool
	loaded  bool
	loading bool
	err     error
	session store.SessionInfo
	events  []model.Event
	// attached counts the attachments by event ID, for the events that
	// are listed (with tools off, the events of tool results are not);
	// attachedEvents lists those events in transcript order.
	attached       map[string][]store.SessionAttachment
	attachedEvents []string

	// keep is the event to return to after a load with the tools toggled:
	// its ID, its time for the fallback when it is no longer listed, and
	// whether it was the marked event.
	keep struct {
		id     string
		ts     time.Time
		marked bool
	}
	// refresh is the position to restore after a reload of the same
	// session (Reload): the marked event, the event at the top with the
	// offset of the first visible line within it, or the bottom.
	refresh struct {
		on       bool
		marked   string
		top      string
		topTS    time.Time
		line     int
		atBottom bool
	}

	lines   []string // transcript, wrapped to the width, plain text
	eventAt []int    // first line of every event
	matches []Match
	hit     int // index into matches, -1 without hits
	current int // marked event, -1 without
	notice  string
}

// New returns the view reading from st and showing times in loc, with the
// effective key bindings km (nil means the defaults).
func New(st *store.Store, loc *time.Location, km keys.Map) Model {
	if loc == nil {
		loc = time.UTC
	}
	k := newKeyMap(km)
	vp := viewport.New()
	vp.KeyMap = k.viewportKeys()
	return Model{st: st, loc: loc, keys: k, vp: vp, hit: -1, current: -1, th: theme.New(true)}
}

// Title implements tui.View.
func (Model) Title() string { return "session" }

// SessionID returns the ID of the session shown, empty before a session is
// loaded.
func (m Model) SessionID() string {
	if !m.loaded {
		return ""
	}
	return m.session.ID
}

// Init implements tea.Model.
func (Model) Init() tea.Cmd { return nil }

// ShortHelp implements help.KeyMap. The attachment key is named only in a
// session that has attachments.
func (m Model) ShortHelp() []key.Binding {
	return m.keys.ShortHelp(len(m.attachedEvents) > 0)
}

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.SetWidth(m.width)
		m.vp.SetHeight(max(m.height-m.chrome(), 0))
		top := m.eventOf(m.vp.YOffset())
		m.layout()
		if top >= 0 {
			m.vp.SetYOffset(m.eventAt[top])
		}
		return m, nil
	case Open:
		m.open = msg
		m.keep.id, m.keep.marked = "", false
		m.refresh.on = false
		return m.load()
	case loadedMsg:
		return m.loadedResult(msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseWheelMsg:
		m.vp, _ = m.vp.Update(msg)
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
		m.render()
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch {
	case key.Matches(msg, m.keys.Back):
		return m, func() tea.Msg { return Back{} }
	case !m.loaded:
		return m, nil
	case key.Matches(msg, m.keys.Copy):
		m.notice = "copied"
		return m, tea.SetClipboard(m.session.ID)
	case key.Matches(msg, m.keys.Attach):
		return m.attach()
	case key.Matches(msg, m.keys.Tools):
		m.all = !m.all
		m.refresh.on = false // a reload in flight is superseded
		if m.current >= 0 {
			m.keep.id, m.keep.ts, m.keep.marked = m.events[m.current].ID, m.events[m.current].TS, true
		} else if top := m.eventOf(m.vp.YOffset()); top >= 0 {
			m.keep.id, m.keep.ts, m.keep.marked = m.events[top].ID, m.events[top].TS, false
		}
		return m.load()
	case key.Matches(msg, m.keys.Next):
		m.jump(1)
	case key.Matches(msg, m.keys.Prev):
		m.jump(-1)
	case key.Matches(msg, m.keys.Top):
		m.vp.GotoTop()
	case key.Matches(msg, m.keys.Bottom):
		m.vp.GotoBottom()
	default:
		m.vp, _ = m.vp.Update(msg)
	}
	return m, nil
}

// Reload reads the shown session again with the same tools setting and
// puts the view back where it was: the marked event stays marked, the
// transcript stays at the same line (new events append below), and a view
// at the bottom follows the end like a pager in follow mode. Before a
// session is loaded or while one is loading there is nothing to reload.
func (m Model) Reload() (tea.Model, tea.Cmd) {
	if !m.loaded || m.loading || m.st == nil {
		return m, nil
	}
	m.refresh.on = true
	m.refresh.marked, m.refresh.top, m.refresh.line = "", "", 0
	m.refresh.atBottom = m.vp.AtBottom()
	if m.current >= 0 && m.current < len(m.events) {
		m.refresh.marked = m.events[m.current].ID
	}
	if top := m.eventOf(m.vp.YOffset()); top >= 0 {
		m.refresh.top, m.refresh.topTS = m.events[top].ID, m.events[top].TS
		m.refresh.line = m.vp.YOffset() - m.eventAt[top]
	}
	return m.load()
}

// load starts reading the session of m.open with the current tools setting.
// It cancels a load still running.
func (m Model) load() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	m.seq++
	m.loading = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	seq, st, ref, all := m.seq, m.st, m.open.SessionID, m.all
	return m, func() tea.Msg {
		defer cancel()
		res := loadedMsg{seq: seq, all: all}
		if st == nil {
			res.err = errors.New("no database")
			return res
		}
		s, err := st.FindSession(ctx, ref)
		switch {
		case errors.Is(err, store.ErrNotFound):
			res.err = fmt.Errorf("no session matches %q", ref)
			return res
		case errors.Is(err, store.ErrAmbiguous):
			res.err = fmt.Errorf("%q matches more than one session; use a longer prefix", ref)
			return res
		case err != nil:
			res.err = err
			return res
		}
		res.session = s
		res.events, res.err = st.Events(ctx, s.ID, all)
		if res.err == nil {
			res.atts, _ = st.SessionAttachmentList(ctx, s.ID)
		}
		return res
	}
}

// loadedResult applies a load result: the transcript is rebuilt, the
// requested event (Open.EventID, or the event kept across a tools toggle)
// is marked and scrolled to, and the first hit from there on is selected.
func (m Model) loadedResult(msg loadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.seq {
		return m, nil
	}
	m.loading = false
	m.err = msg.err
	m.loaded = msg.err == nil
	m.session, m.events, m.all = msg.session, msg.events, msg.all
	m.setAttached(msg.atts)
	refresh := m.refresh.on
	m.refresh.on = false
	if refresh && m.loaded {
		return m.refreshed()
	}
	m.current, m.hit = -1, -1
	m.vp.SetHeight(max(m.height-m.chrome(), 0))
	m.layout()
	if !m.loaded {
		return m, nil
	}
	target, marked := -1, false
	switch {
	case m.keep.id != "":
		target, marked = m.indexOf(m.keep.id), m.keep.marked
		if target < 0 {
			target = m.before(m.keep.ts)
		}
		m.keep.id = ""
	case m.open.EventID != "":
		target = m.indexOf(m.open.EventID)
		if target < 0 && !m.all {
			// Not among the messages, so probably a tool event: switch
			// tools on; the result of that load marks it.
			m.all = true
			return m.load()
		}
		m.open.EventID = ""
		marked = target >= 0
	}
	if target >= 0 {
		m.hit = m.hitFrom(m.eventAt[target])
		if marked {
			m.current = target
		}
		m.vp.GotoTop()
		m.vp.SetYOffset(m.eventAt[target])
	} else if len(m.matches) > 0 {
		m.hit = 0
		m.current = m.eventOf(m.matches[0].Line)
		m.vp.GotoTop()
		m.showHit()
	} else {
		m.vp.GotoTop()
	}
	m.render()
	return m, nil
}

// setAttached groups the attachments by event, keeping those of the
// events that are listed.
func (m *Model) setAttached(atts []store.SessionAttachment) {
	m.attached, m.attachedEvents = nil, nil
	listed := make(map[string]bool, len(m.events))
	for _, e := range m.events {
		listed[e.ID] = true
	}
	for _, a := range atts {
		if !listed[a.EventID] {
			continue
		}
		if m.attached == nil {
			m.attached = map[string][]store.SessionAttachment{}
		}
		if len(m.attached[a.EventID]) == 0 {
			m.attachedEvents = append(m.attachedEvents, a.EventID)
		}
		m.attached[a.EventID] = append(m.attached[a.EventID], a)
	}
}

// attach emits OpenAttachments for the attachments of the session, marked at
// the event shown: the marked event when it has attachments, else the
// first event with some at or below the first visible line, else the last
// one above it. Without attachments it only says so.
func (m Model) attach() (tea.Model, tea.Cmd) {
	if len(m.attachedEvents) == 0 {
		m.notice = "no attachments"
		return m, nil
	}
	id := m.attachedEvents[len(m.attachedEvents)-1]
	switch {
	case m.current >= 0 && len(m.attached[m.events[m.current].ID]) > 0:
		id = m.events[m.current].ID
	default:
		top := m.eventOf(m.vp.YOffset())
		for _, cand := range m.attachedEvents {
			if i := m.indexOf(cand); i >= top {
				id = cand
				break
			}
		}
	}
	o := OpenAttachments{SessionID: m.session.ID, EventID: id, Title: m.session.DisplayName()}
	return m, func() tea.Msg { return o }
}

// refreshed applies a reload of the same session (see Reload): the mark
// and the selected hit are kept where the events still exist, the
// viewport stays on the same line of the same event, or at the bottom
// when it was there.
func (m Model) refreshed() (tea.Model, tea.Cmd) {
	r := m.refresh
	hitLine := -1
	if m.hit >= 0 && m.hit < len(m.matches) {
		hitLine = m.matches[m.hit].Line
	}
	m.vp.SetHeight(max(m.height-m.chrome(), 0))
	m.layout()
	m.current = -1
	if r.marked != "" {
		m.current = m.indexOf(r.marked)
	}
	m.hit = -1
	if hitLine >= 0 {
		m.hit = m.hitFrom(hitLine)
	}
	switch {
	case r.atBottom:
		m.vp.GotoBottom()
	case r.top != "":
		top := m.indexOf(r.top)
		if top < 0 {
			top = m.before(r.topTS)
		}
		if top >= 0 {
			m.vp.SetYOffset(m.eventAt[top] + r.line)
		}
	}
	m.render()
	return m, nil
}

// chrome returns the number of rows the header and the rule take: a
// loaded session with a continuation link in either direction has a third
// header line.
func (m Model) chrome() int {
	if m.loaded && (m.session.ContinuedIn != "" || m.session.ContinuedFrom != "") {
		return chromeHeight + 1
	}
	return chromeHeight
}

// indexOf returns the index of the event with id, -1 if it is not listed.
func (m Model) indexOf(id string) int {
	for i, e := range m.events {
		if e.ID == id {
			return i
		}
	}
	return -1
}

// before returns the index of the last event at or before ts, 0 if none
// precedes it, -1 without events.
func (m Model) before(ts time.Time) int {
	if len(m.events) == 0 {
		return -1
	}
	n := sort.Search(len(m.events), func(i int) bool { return m.events[i].TS.After(ts) })
	return max(n-1, 0)
}

// hitFrom returns the index of the first match on or after line, the last
// match if none follows, -1 without matches.
func (m Model) hitFrom(line int) int {
	if len(m.matches) == 0 {
		return -1
	}
	n := sort.Search(len(m.matches), func(i int) bool { return m.matches[i].Line >= line })
	return min(n, len(m.matches)-1)
}

// eventOf returns the index of the event that contains line, -1 without
// events.
func (m Model) eventOf(line int) int {
	n := sort.Search(len(m.eventAt), func(i int) bool { return m.eventAt[i] > line })
	return n - 1
}

// jump selects the next (delta 1) or previous (delta -1) hit, wrapping
// around, marks its event and scrolls it into view.
func (m *Model) jump(delta int) {
	n := len(m.matches)
	if n == 0 {
		return
	}
	m.hit = ((m.hit+delta)%n + n) % n
	m.current = m.eventOf(m.matches[m.hit].Line)
	m.showHit()
	m.render()
}

// showHit scrolls so the selected hit is visible: with its event's separator
// at the top when the hit is within one screen of it, else with the hit's
// line at the top. A hit already on screen does not scroll.
func (m *Model) showHit() {
	line := m.matches[m.hit].Line
	h := m.vp.Height()
	if line >= m.vp.YOffset() && line < m.vp.YOffset()+h {
		return
	}
	target := m.eventAt[m.current]
	if line-target >= h {
		target = line
	}
	m.vp.SetYOffset(target)
}

// SetKeys replaces the key bindings (the viewport's too), so a changed
// [keys] table applies without a restart; the session and the scroll
// position stay as they are.
func (m Model) SetKeys(km keys.Map) tea.Model {
	m.keys = newKeyMap(km)
	m.vp.KeyMap = m.keys.viewportKeys()
	return m
}
