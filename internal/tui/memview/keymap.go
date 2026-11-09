package memview

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the memory view, built from the effective
// keys (see package keys). Footer and help overlay are generated from it.
// Move exists for the footer only: it names the pair ↑/↓ in one entry.
type KeyMap struct {
	Up, Down, Open, PageUp, PageDown, Back key.Binding
	Move, Scroll                           key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Up:       m.Binding(keys.MemoryUp),
		Down:     m.Binding(keys.MemoryDown),
		Open:     m.Binding(keys.MemoryOpen),
		PageUp:   m.Binding(keys.MemoryPageUp),
		PageDown: m.Binding(keys.MemoryPageDown),
		Back:     m.Binding(keys.GlobalBack),
		Move:     m.Group("move", keys.MemoryUp, keys.MemoryDown),
		Scroll:   m.Group("scroll text", keys.MemoryPageUp, keys.MemoryPageDown),
	}
}

// ShortHelp returns the bindings shown in the footer of a page: the history
// key belongs to the files page only.
func (k KeyMap) ShortHelp(p page) []key.Binding {
	if p == pageHistory {
		return []key.Binding{k.Move, k.Scroll, k.Back}
	}
	return []key.Binding{k.Move, k.Open, k.Scroll, k.Back}
}

// FullHelp returns the bindings shown in the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.PageUp, k.PageDown}, {k.Open, k.Back}}
}
