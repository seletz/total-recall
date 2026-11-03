package store

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// seedHumanEvents writes two repositories, a directory without one and a
// subagent: human, agent, peer and tool events in mixed order.
func seedHumanEvents(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:h1", Harness: "claude", NativeID: "h1", Cwd: "/w/alpha", GitRoot: "/w/alpha", GitRemoteURL: "https://example.test/o/alpha", Name: "Alpha work",
			StartedAt: ts("2026-01-05T08:00:00Z"), EndedAt: ts("2026-01-05T09:00:00Z")},
		{ID: "claude:h2", Harness: "claude", NativeID: "h2", Cwd: "/w/beta", GitRoot: "/w/beta", GitRemoteURL: "https://example.test/o/beta", AITitle: "Beta work",
			StartedAt: ts("2026-01-05T08:30:00Z"), EndedAt: ts("2026-01-05T08:30:00Z")},
		{ID: "claude:h3", Harness: "claude", NativeID: "h3", Cwd: "/w/gamma",
			StartedAt: ts("2026-01-06T08:00:00Z"), EndedAt: ts("2026-01-06T08:00:00Z")},
		{ID: "claude:agent-x", Harness: "claude", NativeID: "agent-x", ParentID: "claude:h1", Cwd: "/w/alpha", AgentName: "researcher",
			StartedAt: ts("2026-01-05T08:10:00Z"), EndedAt: ts("2026-01-05T08:11:00Z")},
	}
	evs := []model.Event{
		ev("claude:h1-2", "claude:h1", "2026-01-05T09:00:00Z", model.ActorHuman, model.KindMessage, "user", "c"),
		ev("claude:h1-1", "claude:h1", "2026-01-05T08:00:00Z", model.ActorHuman, model.KindMessage, "user", "a"),
		ev("claude:h1-a", "claude:h1", "2026-01-05T08:00:05Z", model.ActorAgent, model.KindMessage, "assistant", "b"),
		ev("claude:h1-t", "claude:h1", "2026-01-05T08:00:06Z", model.ActorTool, model.KindToolResult, "tool", "t"),
		ev("claude:h2-1", "claude:h2", "2026-01-05T08:30:00Z", model.ActorHuman, model.KindMessage, "user", "d"),
		ev("claude:h3-1", "claude:h3", "2026-01-06T08:00:00Z", model.ActorHuman, model.KindMessage, "user", "e"),
		ev("claude:x-1", "claude:agent-x", "2026-01-05T08:10:00Z", model.ActorPeer, model.KindMessage, "user", "task"),
		ev("claude:x-2", "claude:agent-x", "2026-01-05T08:11:00Z", model.ActorAgent, model.KindMessage, "assistant", "done"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/h.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func TestHumanEvents(t *testing.T) {
	s, _ := openTestStore(t)
	seedHumanEvents(t, s)
	got, err := s.HumanEvents(ctx, HumanEventsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []HumanEvent{
		{Time: ts("2026-01-05T08:00:00Z"), SessionID: "claude:h1", Title: "Alpha work", Project: "/w/alpha", RemoteURL: "https://example.test/o/alpha"},
		{Time: ts("2026-01-05T08:30:00Z"), SessionID: "claude:h2", Title: "Beta work", Project: "/w/beta", RemoteURL: "https://example.test/o/beta"},
		{Time: ts("2026-01-05T09:00:00Z"), SessionID: "claude:h1", Title: "Alpha work", Project: "/w/alpha", RemoteURL: "https://example.test/o/alpha"},
		{Time: ts("2026-01-06T08:00:00Z"), SessionID: "claude:h3", Title: "", Project: "/w/gamma", Dir: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Time.Equal(want[i].Time) || got[i].SessionID != want[i].SessionID || got[i].Title != want[i].Title ||
			got[i].Project != want[i].Project || got[i].Dir != want[i].Dir || got[i].RemoteURL != want[i].RemoteURL {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// A range selects by time, [Since, Until).
	got, err = s.HumanEvents(ctx, HumanEventsFilter{Since: ts("2026-01-05T08:30:00Z"), Until: ts("2026-01-06T08:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SessionID != "claude:h2" || got[1].SessionID != "claude:h1" {
		t.Errorf("range = %+v", got)
	}
	if got, err := s.HumanEvents(ctx, HumanEventsFilter{Since: ts("2026-02-01T00:00:00Z")}); err != nil || len(got) != 0 {
		t.Errorf("empty range = %+v, %v", got, err)
	}
}
