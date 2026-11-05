package activity

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/keys"
)

// Settings are the configured values the view takes over without a
// restart (see SetSettings): the day boundary, the working time, the
// holidays and the modes the config starts the view in.
type Settings struct {
	// DayStart is the wall-clock time at which a day begins, as an offset
	// from midnight.
	DayStart time.Duration
	// Work is the working time the days are measured against.
	Work store.WorkHours
	// Holidays names the public holidays; nil means there are none.
	Holidays store.Holidays
	// HumanOnly counts own messages only, ByProject colours the bars by
	// project: the view switches to the configured mode.
	HumanOnly bool
	ByProject bool
	// Newest is where the newest day goes, NewestBottom or NewestTop: the
	// view switches to it like o.
	Newest string
}

// SetSettings applies changed settings in place, keeping the window and
// the scroll position: a new working time only redraws the rows (the days
// are the same), a new day boundary or holiday calendar drops the cache
// and loads the window again (the days are counted differently), a new
// counting or colouring mode switches to it like its key, a new place
// for the newest day flips the rows like o. Unchanged settings do
// nothing.
func (m Model) SetSettings(s Settings) (tea.Model, tea.Cmd) {
	reload := s.DayStart != m.opts.DayStart || !sameHolidays(s.Holidays, m.opts.Holidays)
	mode := s.HumanOnly != m.humanOnly || s.ByProject != m.proj.on
	m.opts.DayStart, m.opts.Work, m.opts.Holidays = s.DayStart, s.Work, s.Holidays
	m.humanOnly, m.proj.on = s.HumanOnly, s.ByProject
	if (s.Newest == NewestTop) != m.newestTop {
		m = m.toggleOrder()
	}
	switch {
	case reload:
		return m.Reload()
	case mode:
		return m.load()
	}
	m.clampOffset()
	return m, nil
}

// sameHolidays reports whether two calendars name the same holidays: both
// nil, or both of the same region. A calendar without a region (a test
// double) counts as changed.
func sameHolidays(a, b store.Holidays) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ra, ok1 := a.(interface{ Region() string })
	rb, ok2 := b.(interface{ Region() string })
	return ok1 && ok2 && ra.Region() == rb.Region()
}

// SetKeys replaces the key bindings, so a changed [keys] table applies
// without a restart.
func (m Model) SetKeys(km keys.Map) tea.Model {
	m.opts.Keys = km
	m.keys = newKeyMap(km)
	return m
}
