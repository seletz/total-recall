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
}

// Chrome rows inside the body: two header lines and the status rule.
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

	// keep is the event to return to after a reload (tools toggle): its ID,
	// its time for the fallback when it is no longer listed, and whether it
	// was the marked event.
	keep struct {
		id     string
		ts     time.Time
		marked bool
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

// ShortHelp implements help.KeyMap.
func (m Model) ShortHelp() []key.Binding { return m.keys.ShortHelp() }

// FullHelp implements help.KeyMap.
func (m Model) FullHelp() [][]key.Binding { return m.keys.FullHelp() }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.SetWidth(m.width)
		m.vp.SetHeight(max(m.height-chromeHeight, 0))
		top := m.eventOf(m.vp.YOffset())
		m.layout()
		if top >= 0 {
			m.vp.SetYOffset(m.eventAt[top])
		}
		return m, nil
	case Open:
		m.open = msg
		m.keep.id, m.keep.marked = "", false
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
	case key.Matches(msg, m.keys.Tools):
		m.all = !m.all
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
	m.current, m.hit = -1, -1
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
