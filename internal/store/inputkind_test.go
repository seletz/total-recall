package store

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// TestInputKindRoundTrip writes human events with and without an input
// kind and reads them back through Events and Search.
func TestInputKindRoundTrip(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	evs[0].InputKind = model.InputPasted // e1 human
	// e6 human stays unknown, e4 agent has none.
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	got, err := s.Events(ctx, "claude:s1", true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]model.InputKind{"claude:e1": model.InputPasted}
	for _, e := range got {
		if e.InputKind != want[e.ID] {
			t.Errorf("Events %s InputKind = %q, want %q", e.ID, e.InputKind, want[e.ID])
		}
	}
	hits, err := s.Search(ctx, "login", SearchFilter{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("Search = %v, %v", hits, err)
	}
	for _, h := range hits {
		if h.Event.ID == "claude:e1" && h.Event.InputKind != model.InputPasted {
			t.Errorf("Search hit InputKind = %q", h.Event.InputKind)
		}
	}
}
