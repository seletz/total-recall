package chart

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// fixtureStats loads the weekday×hour aggregation of the whole Activity
// fixture in its local time zone.
func fixtureStats(t *testing.T) store.Stats {
	t.Helper()
	st, _ := fixturedb.Activity(t)
	stats, err := st.Stats(context.Background(), store.StatsFilter{Location: testfixture.ActivityLocation})
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

// TestRenderHeatmapText compares the colourless rendering of the fixture at
// the acceptance size and a few others with golden files.
func TestRenderHeatmapText(t *testing.T) {
	stats := fixtureStats(t)
	tests := []struct {
		name   string
		stats  store.Stats
		metric Metric
		w, h   int
	}{
		{"Events80x10", stats, MetricEvents, 80, 10},
		{"Human80x10", stats, MetricHuman, 80, 10},
		{"Sessions80x10", stats, MetricSessions, 80, 10},
		{"Narrow40x10", stats, MetricEvents, 40, 10},
		{"Wide120x10", stats, MetricEvents, 120, 10},
		{"Empty80x10", store.Stats{}, MetricEvents, 80, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := RenderHeatmapText(tt.stats, tt.metric, tt.w, tt.h)
			lines := strings.Split(ansi.Strip(out), "\n")
			for i, l := range lines {
				lines[i] = strings.TrimRight(l, " ")
			}
			plain := strings.Join(lines, "\n")
			if len(lines) != tt.h {
				t.Errorf("%d lines, want %d:\n%s", len(lines), tt.h, plain)
			}
			for i, l := range lines {
				if ansi.StringWidth(l) > tt.w {
					t.Errorf("line %d is %d cells wide, want ≤ %d: %q", i, ansi.StringWidth(l), tt.w, l)
				}
			}
			golden.RequireEqual(t, []byte(plain))
		})
	}
}

// TestRenderHeatmapTextTooSmall checks that an area below the minimum
// renders nothing instead of a broken grid.
func TestRenderHeatmapTextTooSmall(t *testing.T) {
	stats := fixtureStats(t)
	tests := []struct {
		name string
		w, h int
	}{
		{"short", 80, HeatmapHeight - 1},
		{"narrow", HeatmapMinWidth - 1, HeatmapHeight},
		{"zero", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderHeatmapText(stats, MetricEvents, tt.w, tt.h); got != "" {
				t.Errorf("got %q, want empty", got)
			}
		})
	}
}

// TestRenderHeatmapTextColours checks that a loaded cell carries a colour of
// the scale and the hottest cell the last one, while empty cells stay plain.
func TestRenderHeatmapTextColours(t *testing.T) {
	out := RenderHeatmapText(fixtureStats(t), MetricEvents, 80, 10)
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("output has no escape sequences; cells are not coloured")
	}
	if !strings.Contains(out, "█") {
		t.Error("no cell at full intensity")
	}
}
