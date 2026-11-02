package stats

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/chart"
)

// TestTableSessions checks the two session columns: sessions is the
// distinct count of the window from the series, the session-periods column
// sums the per-period counts, so a session spanning two periods counts
// once in the first and twice in the second. The title of the second
// column names the period.
func TestTableSessions(t *testing.T) {
	series := store.ActivitySeries{
		Keys:        []string{"/w/a", "/w/b"},
		Sessions:    3,
		KeySessions: []int{2, 1},
		Buckets: []store.ActivityBucket{
			{Label: "1", Cells: []store.ActivityCell{
				{Key: "/w/a", Activity: store.Activity{Sessions: 2, Human: 2, Agent: 1}},
				{Key: "/w/b", Activity: store.Activity{Sessions: 1, Human: 1}},
			}},
			{Label: "2", Cells: []store.ActivityCell{
				{Key: "/w/a", Activity: store.Activity{Sessions: 1, Human: 1}}, // one session of a continues
				{Key: "/w/b"},
			}},
		},
	}
	tests := []struct {
		period store.Period
		title  string
	}{
		{store.PeriodDay, "session-days"},
		{store.PeriodWeek, "session-weeks"},
		{store.PeriodMonth, "session-months"},
	}
	for _, tt := range tests {
		t.Run(tt.period.String(), func(t *testing.T) {
			spec := chart.FromSeries(series, tt.period, chart.MetricEvents, map[string]string{"/w/a": "a", "/w/b": "b"})
			lines := strings.Split(strings.TrimRight(ansi.Strip(newTable(spec, series, tt.period, store.DimProject, 80).View()), "\n"), "\n")
			if len(lines) != 3 {
				t.Fatalf("got %d lines, want 3:\n%s", len(lines), strings.Join(lines, "\n"))
			}
			want := []string{
				"project sessions " + tt.title + " human agent share",
				"a 2 3 3 1 80 %",
				"b 1 1 1 0 20 %",
			}
			for i, l := range lines {
				if got := strings.Join(strings.Fields(l), " "); got != want[i] {
					t.Errorf("line %d = %q, want %q", i, got, want[i])
				}
			}
		})
	}
}
