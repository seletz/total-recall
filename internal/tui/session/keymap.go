package session

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
)

// KeyMap holds the bindings of the session view. Footer and help overlay are
// generated from it. Scroll and Hit exist for the footer only: they name the
// pairs ↑/↓ and n/N in one entry each.
type KeyMap struct {
	Up, Down, PageUp, PageDown, HalfUp, HalfDown, Top, Bottom key.Binding
	Next, Prev, Tools, Copy, Back                             key.Binding
	Scroll, Hit                                               key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("b/pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "f", "space"), key.WithHelp("f/pgdn", "page down")),
		HalfUp:   key.NewBinding(key.WithKeys("u", "ctrl+u"), key.WithHelp("u", "½ page up")),
		HalfDown: key.NewBinding(key.WithKeys("d", "ctrl+d"), key.WithHelp("d", "½ page down")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/home", "top")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G/end", "bottom")),
		Next:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next hit")),
		Prev:     key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "previous hit")),
		Tools:    key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "tools on/off")),
		Copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy session id")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Scroll:   key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "scroll")),
		Hit:      key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n/N", "next/prev hit")),
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

// ShortHelp returns the bindings shown in the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Scroll, k.Hit, k.Tools, k.Copy, k.Back}
}

// FullHelp returns the bindings shown in the help overlay: scrolling in the
// first column, actions in the second.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.HalfUp, k.HalfDown, k.Top, k.Bottom},
		{k.Next, k.Prev, k.Tools, k.Copy, k.Back},
	}
}
