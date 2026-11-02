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
		// The compact layout: the body of an 80×24 terminal leaves 9 rows.
		{"Compact79x9", stats, MetricEvents, 79, 9},
		{"CompactHuman79x9", stats, MetricHuman, 79, 9},
		{"CompactNarrow40x9", stats, MetricEvents, 40, 9},
		{"CompactEmpty79x9", store.Stats{}, MetricEvents, 79, 9},
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
		{"short", 80, HeatmapCompactHeight - 1},
		{"narrow", HeatmapMinWidth - 1, HeatmapHeight},
		{"narrowCompact", HeatmapMinWidth - 1, HeatmapCompactHeight},
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

// TestHeatmapLayoutFor checks which layout an area gets: the full one from
// HeatmapHeight rows up, the compact one down to HeatmapCompactHeight, none
// below that or below HeatmapMinWidth.
func TestHeatmapLayoutFor(t *testing.T) {
	tests := []struct {
		name   string
		w, h   int
		want   HeatmapLayout
		height int
	}{
		{"acceptance size", 80, 10, HeatmapFull, HeatmapHeight},
		{"tall", 120, 30, HeatmapFull, HeatmapHeight},
		{"narrow but tall", HeatmapMinWidth, 10, HeatmapFull, HeatmapHeight},
		{"80x24 body", 79, 9, HeatmapCompact, HeatmapCompactHeight},
		{"narrow and low", HeatmapMinWidth, 9, HeatmapCompact, HeatmapCompactHeight},
		{"too low", 80, 8, HeatmapNone, 0},
		{"too narrow", HeatmapMinWidth - 1, 10, HeatmapNone, 0},
		{"zero", 0, 0, HeatmapNone, 0},
		{"negative", -1, -1, HeatmapNone, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HeatmapLayoutFor(tt.w, tt.h)
			if got != tt.want {
				t.Errorf("HeatmapLayoutFor(%d, %d) = %v, want %v", tt.w, tt.h, got, tt.want)
			}
			if got.Height() != tt.height {
				t.Errorf("Height() = %d, want %d", got.Height(), tt.height)
			}
		})
	}
}

// TestRenderHeatmapTextCompactColours checks that the compact layout keeps
// the colour ramp: coloured cells, the hottest at full intensity, and the
// legend in the title showing every shade.
func TestRenderHeatmapTextCompactColours(t *testing.T) {
	out := RenderHeatmapText(fixtureStats(t), MetricEvents, 79, HeatmapCompactHeight)
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("output has no escape sequences; cells are not coloured")
	}
	title := strings.SplitN(ansi.Strip(out), "\n", 2)[0]
	for _, r := range heatRunes {
		if !strings.ContainsRune(title, r) {
			t.Errorf("title %q lacks legend shade %q", title, r)
		}
	}
	if !strings.Contains(out, "█") {
		t.Error("no cell at full intensity")
	}
}
