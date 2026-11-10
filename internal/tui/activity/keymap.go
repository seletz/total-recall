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
	Figures          key.Binding
	Projects         key.Binding
	Order            key.Binding
	Up, Down         key.Binding
	SelUp, SelDown   key.Binding
	Open             key.Binding
	// Footer groups.
	Period, Range, Quick key.Binding
	Select               key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Day:      m.Binding(keys.ActivityDay),
		Week:     m.Binding(keys.ActivityWeek),
		Month:    m.Binding(keys.ActivityMonth),
		Earlier:  m.Binding(keys.ActivityEarlier),
		Later:    m.Binding(keys.ActivityLater),
		Current:  m.Binding(keys.ActivityCurrent),
		Last:     m.Binding(keys.ActivityLast),
		Human:    m.Binding(keys.ActivityHuman),
		Figures:  m.Binding(keys.ActivityFigures),
		Projects: m.Binding(keys.ActivityProjects),
		Order:    m.Binding(keys.ActivityOrder),
		Up:       m.Binding(keys.ActivityUp),
		Down:     m.Binding(keys.ActivityDown),
		SelUp:    m.Binding(keys.ActivitySelUp),
		SelDown:  m.Binding(keys.ActivitySelDown),
		Open:     m.Binding(keys.ActivityOpen),
		Period:   m.Group("period", keys.ActivityDay, keys.ActivityWeek, keys.ActivityMonth),
		Range:    m.Group("earlier/later", keys.ActivityEarlier, keys.ActivityLater),
		Quick:    m.Group("this/last", keys.ActivityCurrent, keys.ActivityLast),
		Select:   m.Group("select day", keys.ActivitySelUp, keys.ActivitySelDown),
	}
}

// ShortHelp returns the grouped bindings for the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Period, k.Range, k.Quick, k.Select, k.Open, k.Human, k.Figures, k.Projects, k.Order}
}

// FullHelp returns every binding for the help overlay, in three columns.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Day, k.Week, k.Month, k.Human},
		{k.Earlier, k.Later, k.Current, k.Last},
		{k.Figures, k.Projects, k.Order, k.Up, k.Down},
		{k.SelUp, k.SelDown, k.Open},
	}
}
