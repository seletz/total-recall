package session

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the session view, built from the effective
// keys (see package keys). Footer and help overlay are generated from it.
// Scroll and Hit exist for the footer only: they name the pairs ↑/↓ and
// n/N in one entry each.
type KeyMap struct {
	Up, Down, PageUp, PageDown, HalfUp, HalfDown, Top, Bottom key.Binding
	Next, Prev, Tools, Copy, Attach, Back                     key.Binding
	Scroll, Hit                                               key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Up:       m.Binding(keys.SessionUp),
		Down:     m.Binding(keys.SessionDown),
		PageUp:   m.Binding(keys.SessionPageUp),
		PageDown: m.Binding(keys.SessionPageDown),
		HalfUp:   m.Binding(keys.SessionHalfUp),
		HalfDown: m.Binding(keys.SessionHalfDown),
		Top:      m.Binding(keys.SessionTop),
		Bottom:   m.Binding(keys.SessionBottom),
		Next:     m.Binding(keys.SessionNext),
		Prev:     m.Binding(keys.SessionPrev),
		Tools:    m.Binding(keys.SessionTools),
		Copy:     m.Binding(keys.SessionCopy),
		Attach:   m.Binding(keys.SessionAttach),
		Back:     m.Binding(keys.GlobalBack),
		Scroll:   m.Group("scroll", keys.SessionUp, keys.SessionDown),
		Hit:      m.Group("next/prev hit", keys.SessionNext, keys.SessionPrev),
	}
}

// viewportKeys maps the scroll bindings onto the viewport; horizontal
// scrolling is off because lines are wrapped to the width.
func (k KeyMap) viewportKeys() viewport.KeyMap {
	off := key.NewBinding(key.WithDisabled())
	return viewport.KeyMap{
		Up: k.Up, Down: k.Down, PageUp: k.PageUp, PageDown: k.PageDown,
		HalfPageUp: k.HalfUp, HalfPageDown: k.HalfDown, Left: off, Right: off,
	}
}

// ShortHelp returns the bindings shown in the footer; attach says whether
// the session has attachments, which the footer names only then.
func (k KeyMap) ShortHelp(attach bool) []key.Binding {
	help := []key.Binding{k.Scroll, k.Hit, k.Tools, k.Copy}
	if attach {
		help = append(help, k.Attach)
	}
	return append(help, k.Back)
}

// FullHelp returns the bindings shown in the help overlay: scrolling in the
// first column, actions in the second.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.HalfUp, k.HalfDown, k.Top, k.Bottom},
		{k.Next, k.Prev, k.Tools, k.Copy, k.Attach, k.Back},
	}
}
