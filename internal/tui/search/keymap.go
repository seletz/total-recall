package search

import (
	"slices"

	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the search view, built from the effective
// keys (see package keys). The footer and the help overlay are generated
// from it. Move exists for the footer only: it names the pair ↑/↓ in one
// entry.
//
// Focus: List and Input move the focus between the input and the hit list
// with the focus keys (tab and shift+tab by default, both directions, the
// view never gives them to the app); the back key (esc) is the "one level
// back" of Input and Cancel. See docs/reference/30-tui.md, section Fokus.
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
	// TypeEvent, TypeSession and TypeMemory toggle the object types of the
	// search, Types names the three in the footer; Pin pins the project of
	// the marked hit (or drops the pin) and Project chooses one from the
	// list.
	TypeEvent, TypeSession, TypeMemory key.Binding
	Types                              key.Binding
	Pin, Project                       key.Binding
	// Attach opens the attachments of the marked event.
	Attach key.Binding
	// Confirm and Cancel apply while the repo filter is typed.
	Confirm key.Binding
	Cancel  key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	focus, back, open := m.Keys(keys.SearchFocus), m.Keys(keys.GlobalBack), m.Keys(keys.SearchOpen)
	return KeyMap{
		Up:    m.Binding(keys.SearchUp),
		Down:  m.Binding(keys.SearchDown),
		Open:  m.Binding(keys.SearchOpen),
		List:  key.NewBinding(key.WithKeys(focus...), key.WithHelp(keys.Display(focus[0]), "list")),
		Input: key.NewBinding(key.WithKeys(slices.Concat(focus, back)...), key.WithHelp(keys.Display(focus[0])+"/"+keys.Display(back[0]), "input")),
		Actor: m.Binding(keys.SearchActor),
		Time:  m.Binding(keys.SearchTime),
		Repo:  m.Binding(keys.SearchRepo),
		Raw:   m.Binding(keys.SearchRaw),
		Move:  m.Group("move", keys.SearchUp, keys.SearchDown),

		TypeEvent:   m.Binding(keys.SearchTypeEvent),
		TypeSession: m.Binding(keys.SearchTypeSession),
		TypeMemory:  m.Binding(keys.SearchTypeMemory),
		Types:       m.Group("types", keys.SearchTypeEvent, keys.SearchTypeSession, keys.SearchTypeMemory),
		Pin:         m.Binding(keys.SearchPin),
		Project:     m.Binding(keys.SearchProject),
		Attach:      m.Binding(keys.SearchAttach),

		Confirm: key.NewBinding(key.WithKeys(slices.Concat(open, focus)...), key.WithHelp(keys.Display(open[0]), "apply")),
		Cancel:  key.NewBinding(key.WithKeys(back...), key.WithHelp(keys.Display(back[0]), "cancel")),
	}
}
