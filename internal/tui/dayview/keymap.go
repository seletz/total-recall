package dayview

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the day view, built from the effective keys
// (see package keys). Move and Day exist for the footer only: they name
// the pairs in one entry.
type KeyMap struct {
	Up, Down, Prev, Next, Open, Human, Back key.Binding
	Move, Day                               key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Up:    m.Binding(keys.DayUp),
		Down:  m.Binding(keys.DayDown),
		Prev:  m.Binding(keys.DayPrev),
		Next:  m.Binding(keys.DayNext),
		Open:  m.Binding(keys.DayOpen),
		Human: m.Binding(keys.DayHuman),
		Back:  m.Binding(keys.GlobalBack),
		Move:  m.Group("move", keys.DayUp, keys.DayDown),
		Day:   m.Group("prev/next day", keys.DayPrev, keys.DayNext),
	}
}

// ShortHelp returns the bindings of the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Move, k.Open, k.Day, k.Human, k.Back}
}

// FullHelp returns every binding for the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Open}, {k.Prev, k.Next, k.Human, k.Back}}
}
