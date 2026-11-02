package search

import "charm.land/bubbles/v2/key"

// KeyMap holds the bindings of the search view. The footer and the help
// overlay are generated from it. Move exists for the footer only: it names
// the pair ↑/↓ in one entry.
//
// Focus: List and Input move the focus between the input and the hit list
// with tab and shift+tab (both directions, the view never gives tab to the
// app); esc is the "one level back" of Input and Cancel. See docs/tui.md,
// section Fokus.
type KeyMap struct {
	Up    key.Binding
	Down  key.Binding
	Open  key.Binding
	List  key.Binding
	Input key.Binding
	Actor key.Binding
	Time  key.Binding
	Repo  key.Binding
	Raw   key.Binding
	Move  key.Binding
	// Confirm and Cancel apply while the repo filter is typed.
	Confirm key.Binding
	Cancel  key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		List:    key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "list")),
		Input:   key.NewBinding(key.WithKeys("tab", "shift+tab", "esc"), key.WithHelp("tab/esc", "input")),
		Actor:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "actor")),
		Time:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "time")),
		Repo:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repo")),
		Raw:     key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "raw fts")),
		Move:    key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		Confirm: key.NewBinding(key.WithKeys("enter", "tab", "shift+tab"), key.WithHelp("enter", "apply")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}
