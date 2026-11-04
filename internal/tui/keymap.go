package tui

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the global bindings of the app shell, built from the
// effective keys (see package keys). The help overlay and the footer are
// generated from it. Views are switched by number only: tab belongs to the
// views, which use it to move the focus between their elements.
type KeyMap struct {
	Search   key.Binding
	Stats    key.Binding
	Activity key.Binding
	Help     key.Binding
	Back     key.Binding
	Quit     key.Binding
	// Reload loads the current view again right away, whatever the
	// reload interval is.
	Reload key.Binding
	// ForceQuit quits in every state, even while a view holds the
	// keyboard; it is fixed to ctrl+c.
	ForceQuit key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Search:    m.Binding(keys.GlobalSearch),
		Stats:     m.Binding(keys.GlobalStats),
		Activity:  m.Binding(keys.GlobalActivity),
		Help:      m.Binding(keys.GlobalHelp),
		Back:      m.Binding(keys.GlobalBack),
		Quit:      m.Binding(keys.GlobalQuit),
		Reload:    m.Binding(keys.GlobalReload),
		ForceQuit: keys.ForceQuit,
	}
}

// ShortHelp returns the bindings shown in the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Search, k.Stats, k.Activity, k.Help, k.Quit}
}

// FullHelp returns the bindings shown in the help overlay, as two columns.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Search, k.Stats, k.Activity, k.Reload}, {k.Help, k.Back, k.Quit, k.ForceQuit}}
}
