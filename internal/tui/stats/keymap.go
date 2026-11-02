package stats

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the stats view, built from the effective
// keys (see package keys). The footer shows the grouped bindings (Period,
// Dimension, Range), the help overlay every single one.
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

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Day:       m.Binding(keys.StatsDays),
		Week:      m.Binding(keys.StatsWeeks),
		Month:     m.Binding(keys.StatsMonths),
		Project:   m.Binding(keys.StatsProject),
		Agent:     m.Binding(keys.StatsAgent),
		Directory: m.Binding(keys.StatsDirectory),
		Earlier:   m.Binding(keys.StatsEarlier),
		Later:     m.Binding(keys.StatsLater),
		ZoomOut:   m.Binding(keys.StatsZoomOut),
		ZoomIn:    m.Binding(keys.StatsZoomIn),
		Metric:    m.Binding(keys.StatsMetric),
		Heatmap:   m.Binding(keys.StatsHeatmap),
		Graphics:  m.Binding(keys.StatsGraphics),
		Period:    m.Group("period", keys.StatsDays, keys.StatsWeeks, keys.StatsMonths),
		Dimension: m.Group("by", keys.StatsProject, keys.StatsAgent, keys.StatsDirectory),
		Range:     m.Group("range", keys.StatsEarlier, keys.StatsLater, keys.StatsZoomOut, keys.StatsZoomIn),
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
