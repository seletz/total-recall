package memtab

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the memory tab: the keys of the memory
// viewer for moving, the open key under its own description.
type KeyMap struct {
	Up, Down, PageUp, PageDown, Open key.Binding
	Move                             key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	open := m.Binding(keys.MemoryOpen)
	return KeyMap{
		Up:       m.Binding(keys.MemoryUp),
		Down:     m.Binding(keys.MemoryDown),
		PageUp:   m.Binding(keys.MemoryPageUp),
		PageDown: m.Binding(keys.MemoryPageDown),
		Open:     key.NewBinding(key.WithKeys(open.Keys()...), key.WithHelp(open.Help().Key, "open memory")),
		Move:     m.Group("move", keys.MemoryUp, keys.MemoryDown),
	}
}

// ShortHelp returns the bindings of the footer.
func (k KeyMap) ShortHelp() []key.Binding { return []key.Binding{k.Move, k.Open} }

// FullHelp returns the bindings of the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.PageUp, k.PageDown, k.Open}}
}
