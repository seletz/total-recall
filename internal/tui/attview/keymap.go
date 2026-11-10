package attview

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the attachment view, built from the
// effective keys (see package keys). Footer and help overlay are
// generated from it. Browse and Scroll exist for the footer only: each
// names a pair of keys in one entry.
type KeyMap struct {
	Prev, Next, Up, Down, PageUp, PageDown, Open, Back key.Binding
	Browse, Scroll                                     key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Prev:     m.Binding(keys.AttachPrev),
		Next:     m.Binding(keys.AttachNext),
		Up:       m.Binding(keys.AttachUp),
		Down:     m.Binding(keys.AttachDown),
		PageUp:   m.Binding(keys.AttachPageUp),
		PageDown: m.Binding(keys.AttachPageDown),
		Open:     m.Binding(keys.AttachOpen),
		Back:     m.Binding(keys.GlobalBack),
		Browse:   m.Group("browse", keys.AttachPrev, keys.AttachNext),
		Scroll:   m.Group("scroll", keys.AttachUp, keys.AttachDown),
	}
}

// ShortHelp returns the bindings shown in the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Browse, k.Scroll, k.Open, k.Back}
}

// FullHelp returns the bindings shown in the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Prev, k.Next, k.Up, k.Down, k.PageUp, k.PageDown}, {k.Open, k.Back}}
}
