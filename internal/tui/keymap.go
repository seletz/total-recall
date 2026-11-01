package tui

import "charm.land/bubbles/v2/key"

// KeyMap holds the global bindings of the app shell. The help overlay and
// the footer are generated from it, so it is the single place to edit.
type KeyMap struct {
	Search key.Binding
	Stats  key.Binding
	Next   key.Binding
	Help   key.Binding
	Back   key.Binding
	Quit   key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Search: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "search")),
		Stats:  key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "stats")),
		Next:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next view")),
		Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp returns the bindings shown in the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Search, k.Stats, k.Next, k.Help, k.Quit}
}

// FullHelp returns the bindings shown in the help overlay, as one column.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Search, k.Stats, k.Next}, {k.Help, k.Back, k.Quit}}
}
