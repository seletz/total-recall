package search

import "charm.land/bubbles/v2/key"

// KeyMap holds the bindings of the search view. The footer and the help
// overlay are generated from it.
type KeyMap struct {
	Up    key.Binding
	Down  key.Binding
	Open  key.Binding
	List  key.Binding
	Input key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Up:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Open:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		List:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "list")),
		Input: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "input")),
	}
}
