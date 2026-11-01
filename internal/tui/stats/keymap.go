package stats

import "charm.land/bubbles/v2/key"

// KeyMap holds the bindings of the stats view. The footer shows the grouped
// bindings (Period, Dimension, Range), the help overlay every single one.
// None of the keys is one the app shell handles itself.
type KeyMap struct {
	Day, Week, Month          key.Binding
	Project, Agent, Directory key.Binding
	Earlier, Later            key.Binding
	ZoomOut, ZoomIn           key.Binding
	Metric                    key.Binding
	Heatmap                   key.Binding
	Graphics                  key.Binding
	// Footer groups.
	Period, Dimension, Range key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Day:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "days")),
		Week:      key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "weeks")),
		Month:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "months")),
		Project:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "by project")),
		Agent:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "by agent")),
		Directory: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "by directory")),
		Earlier:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "earlier")),
		Later:     key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "later")),
		ZoomOut:   key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "zoom out")),
		ZoomIn:    key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "zoom in")),
		Metric:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "metric")),
		Heatmap:   key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "heatmap")),
		Graphics:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "graphics")),
		Period:    key.NewBinding(key.WithKeys("d", "w", "m"), key.WithHelp("d/w/m", "period")),
		Dimension: key.NewBinding(key.WithKeys("p", "a", "v"), key.WithHelp("p/a/v", "by")),
		Range:     key.NewBinding(key.WithKeys("[", "]", "-", "+", "="), key.WithHelp("[ ] -/+", "range")),
	}
}

// ShortHelp returns the grouped bindings for the footer. The metric and
// heatmap keys are left out so the line fits beside the global keys in 100
// columns; the chart title names the active metric, the status line the
// heatmap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Period, k.Dimension, k.Range}
}

// FullHelp returns every binding for the help overlay, in three columns.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Day, k.Week, k.Month, k.Metric},
		{k.Project, k.Agent, k.Directory, k.Heatmap},
		{k.Earlier, k.Later, k.ZoomOut, k.ZoomIn, k.Graphics},
	}
}
