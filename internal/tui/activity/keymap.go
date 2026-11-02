package activity

import "charm.land/bubbles/v2/key"

// KeyMap holds the bindings of the activity view. The footer shows the
// grouped bindings, the help overlay every single one. None of the keys is
// one the app shell handles itself.
type KeyMap struct {
	Day, Week, Month key.Binding
	Earlier, Later   key.Binding
	Current, Last    key.Binding
	Human            key.Binding
	Figures          key.Binding
	Up, Down         key.Binding
	// Footer groups.
	Period, Range, Quick key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Day:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "one day")),
		Week:    key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "one week")),
		Month:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "one month")),
		Earlier: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "earlier")),
		Later:   key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "later")),
		Current: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "this day/week/month")),
		Last:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "last day/week/month")),
		Human:   key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "human only")),
		Figures: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "summary figures")),
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
		Period:  key.NewBinding(key.WithKeys("d", "w", "m"), key.WithHelp("d/w/m", "period")),
		Range:   key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[ ]", "earlier/later")),
		Quick:   key.NewBinding(key.WithKeys("c", "l"), key.WithHelp("c/l", "this/last")),
	}
}

// ShortHelp returns the grouped bindings for the footer.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Period, k.Range, k.Quick, k.Human, k.Figures}
}

// FullHelp returns every binding for the help overlay, in three columns.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Day, k.Week, k.Month, k.Human},
		{k.Earlier, k.Later, k.Current, k.Last},
		{k.Figures, k.Up, k.Down},
	}
}
