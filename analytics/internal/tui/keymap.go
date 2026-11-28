package tui

import "charm.land/bubbles/v2/key"

// keyMap are the keys of the view.
type keyMap struct {
	Metric, MetricBack, By, Period, Host key.Binding
	Earlier, Later, First, Last          key.Binding
	Reload, Help, Back, Quit             key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Metric:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "show")),
		MetricBack: key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "show back")),
		By:         key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "by")),
		Period:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "period")),
		Host:       key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "host")),
		Earlier:    key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "earlier")),
		Later:      key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "later")),
		First:      key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first")),
		Last:       key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last")),
		Reload:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// short is the footer: the actions of the view first, then help and quit.
func (k keyMap) short() []key.Binding {
	return []key.Binding{k.Metric, k.By, k.Period, k.Host, k.Earlier, k.Later, k.Reload, k.Help, k.Quit}
}

// full is the help panel.
func (k keyMap) full() [][]key.Binding {
	return [][]key.Binding{
		{k.Metric, k.MetricBack, k.By, k.Period, k.Host},
		{k.Earlier, k.Later, k.First, k.Last},
		{k.Reload, k.Help, k.Back, k.Quit},
	}
}
