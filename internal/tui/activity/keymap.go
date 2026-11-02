package activity

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the activity view, built from the effective
// keys (see package keys). The footer shows the grouped bindings, the help
// overlay every single one.
type KeyMap struct {
	Day, Week, Month key.Binding
	Earlier, Later   key.Binding
	Current, Last    key.Binding
	Human            key.Binding
	Up, Down         key.Binding
	// Footer groups.
	Period, Range, Quick key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Day:     m.Binding(keys.ActivityDay),
		Week:    m.Binding(keys.ActivityWeek),
		Month:   m.Binding(keys.ActivityMonth),
		Earlier: m.Binding(keys.ActivityEarlier),
		Later:   m.Binding(keys.ActivityLater),
		Current: m.Binding(keys.ActivityCurrent),
		Last:    m.Binding(keys.ActivityLast),
		Human:   m.Binding(keys.ActivityHuman),
		Up:      m.Binding(keys.ActivityUp),
		Down:    m.Binding(keys.ActivityDown),
		Period:  m.Group("period", keys.ActivityDay, keys.ActivityWeek, keys.ActivityMonth),
		Range:   m.Group("earlier/later", keys.ActivityEarlier, keys.ActivityLater),
		Quick:   m.Group("this/last", keys.ActivityCurrent, keys.ActivityLast),
	}
}

// ShortHelp returns the grouped bindings for the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Period, k.Range, k.Quick, k.Human}
}

// FullHelp returns every binding for the help overlay, in three columns.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Day, k.Week, k.Month, k.Human},
		{k.Earlier, k.Later, k.Current, k.Last},
		{k.Up, k.Down},
	}
}
