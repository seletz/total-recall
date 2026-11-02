package tui

import "charm.land/bubbles/v2/key"

// KeyMap holds the global bindings of the app shell. The help overlay and
// the footer are generated from it, so it is the single place to edit.
// Views are switched by number only: tab belongs to the views, which use it
// to move the focus between their elements.
type KeyMap struct {
	Search   key.Binding
	Stats    key.Binding
	Activity key.Binding
	Help     key.Binding
	Back     key.Binding
	Quit     key.Binding
	// ForceQuit is the quit hint shown while a view holds the keyboard.
	ForceQuit key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Search:    key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "search")),
		Stats:     key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "stats")),
		Activity:  key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "activity")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}

// ShortHelp returns the bindings shown in the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Search, k.Stats, k.Activity, k.Help, k.Quit}
}

// FullHelp returns the bindings shown in the help overlay, as one column.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Search, k.Stats, k.Activity}, {k.Help, k.Back, k.Quit}}
}
