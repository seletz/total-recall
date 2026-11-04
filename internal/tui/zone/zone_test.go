package zone

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestToggles(t *testing.T) {
	names := []string{"day", "week", "month"}
	tests := []struct {
		name   string
		active int
		offset int
		text   string
		spans  []Span
	}{
		{"first active", 0, 0, "[day] week month", []Span{{0, 5}, {6, 10}, {11, 16}}},
		{"last active", 2, 0, "day week [month]", []Span{{0, 3}, {4, 8}, {9, 16}}},
		{"offset", 1, 9, "day [week] month", []Span{{9, 12}, {13, 19}, {20, 25}}},
		{"none active", -1, 0, "day week month", []Span{{0, 3}, {4, 8}, {9, 14}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, spans := Toggles(lipgloss.NewStyle().Bold(true), names, tt.active, tt.offset)
			if got := ansi.Strip(text); got != tt.text {
				t.Errorf("text = %q, want %q", got, tt.text)
			}
			if len(spans) != len(tt.spans) {
				t.Fatalf("spans = %v, want %v", spans, tt.spans)
			}
			for i := range spans {
				if spans[i] != tt.spans[i] {
					t.Errorf("span %d = %v, want %v", i, spans[i], tt.spans[i])
				}
			}
		})
	}
}

func TestAt(t *testing.T) {
	spans := []Span{{1, 4}, {6, 7}}
	tests := []struct {
		x    int
		want int
	}{
		{0, -1}, {1, 0}, {3, 0}, {4, -1}, {5, -1}, {6, 1}, {7, -1},
	}
	for _, tt := range tests {
		if got := At(spans, tt.x); got != tt.want {
			t.Errorf("At(%d) = %d, want %d", tt.x, got, tt.want)
		}
	}
}

// TestClip checks that a span cut off by the width shrinks and one beyond it
// becomes empty, so a cut-off element is never clickable.
func TestClip(t *testing.T) {
	spans := Clip([]Span{{0, 3}, {4, 8}, {9, 12}}, 6)
	want := []Span{{0, 3}, {4, 6}, {6, 6}}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %v, want %v", i, spans[i], want[i])
		}
	}
	if At(spans, 9) != -1 || At(spans, 6) != -1 {
		t.Errorf("clipped spans still hit: %v", spans)
	}
}

func TestRect(t *testing.T) {
	r := Rect{X: 2, Y: 1, W: 3, H: 2}
	tests := []struct {
		x, y int
		want bool
	}{
		{2, 1, true}, {4, 2, true}, {1, 1, false}, {5, 1, false}, {2, 0, false}, {2, 3, false},
	}
	for _, tt := range tests {
		if got := r.Contains(tt.x, tt.y); got != tt.want {
			t.Errorf("Contains(%d, %d) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
}
