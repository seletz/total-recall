package ops

import (
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/ingest"
)

func TestFormatReclassifyInputKinds(t *testing.T) {
	tests := []struct {
		name string
		s    ingest.ReclassifySummary
		want string
	}{
		{"no input kinds", ingest.ReclassifySummary{Examined: 5, Transitions: map[string]int{}}, "reindex --actors: 5 user messages, 0 changed (0s)"},
		{"input kinds", ingest.ReclassifySummary{Examined: 5, InputKinds: 4, Duration: time.Second, Transitions: map[string]int{}}, "reindex --actors: 5 user messages, 0 changed, 4 input kinds set (1s)"},
		{"dry run", ingest.ReclassifySummary{Examined: 5, InputKinds: 4, DryRun: true, Transitions: map[string]int{}}, "reindex --actors (dry run): 5 user messages, 0 would change, 4 input kinds would be set (0s)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatReclassify(tt.s); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
