package store

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// TestSearchEventActors: the event group says how many of its matches each
// actor has, peer and automation included, so the numbers of the types add
// up in front of the user (#383): the counts are of all matches, also past
// the limit, and sum to Total.
func TestSearchEventActors(t *testing.T) {
	s, _ := openTestStore(t)
	sess := []model.Session{{ID: "claude:a1", Harness: "claude", NativeID: "a1", Cwd: "/w/app", GitRoot: "/w/app",
		StartedAt: ts("2026-10-31T10:00:00Z"), EndedAt: ts("2026-10-31T11:00:00Z")}}
	evs := []model.Event{
		ev("claude:a1-1", "claude:a1", "2026-10-31T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "lighthouse one"),
		ev("claude:a1-2", "claude:a1", "2026-10-31T10:01:00Z", model.ActorAgent, model.KindMessage, "assistant", "lighthouse two"),
		ev("claude:a1-3", "claude:a1", "2026-10-31T10:02:00Z", model.ActorAgent, model.KindMessage, "assistant", "lighthouse three"),
		ev("claude:a1-4", "claude:a1", "2026-10-31T10:03:00Z", model.ActorPeer, model.KindMessage, "user", "lighthouse four"),
		ev("claude:a1-5", "claude:a1", "2026-10-31T10:04:00Z", model.ActorAutomation, model.KindMessage, "user", "lighthouse five"),
		ev("claude:a1-6", "claude:a1", "2026-10-31T10:05:00Z", model.ActorTool, model.KindToolResult, "tool", "lighthouse six"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 10}, sess, evs); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 2} {
		groups, err := s.SearchTyped(ctx, TypedQuery{Text: "lighthouse", Types: []HitType{TypeEvent, TypeInput}, SessionFilter: SessionFilter{Limit: limit}})
		if err != nil {
			t.Fatal(err)
		}
		g := groups[0]
		want := []ActorCount{{"human", 1}, {"agent", 2}, {"peer", 1}, {"automation", 1}, {"tool", 1}}
		if len(g.Actors) != len(want) {
			t.Fatalf("limit %d: actors = %v, want %v", limit, g.Actors, want)
		}
		sum := 0
		for i, a := range g.Actors {
			if a != want[i] {
				t.Errorf("limit %d: actor %d = %v, want %v", limit, i, a, want[i])
			}
			sum += a.N
		}
		if sum != g.Total || g.Total != 6 {
			t.Errorf("limit %d: the actors sum to %d, Total = %d, want 6", limit, sum, g.Total)
		}
		if len(groups[1].Actors) != 0 {
			t.Errorf("input group has actors %v", groups[1].Actors)
		}
	}
}
