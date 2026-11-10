package store

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

func seedUserMessages(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:m1", Harness: "claude", NativeID: "m1", Cwd: "/w/m1-dir", GitBranch: "feat/m1", StartedAt: ts("2026-01-05T08:00:00Z"), EndedAt: ts("2026-01-05T09:00:00Z")},
		{ID: "claude:agent-y", Harness: "claude", NativeID: "agent-y", ParentID: "claude:m1", StartedAt: ts("2026-01-05T08:10:00Z"), EndedAt: ts("2026-01-05T08:11:00Z")},
		{ID: "other:o1", Harness: "other", NativeID: "o1", StartedAt: ts("2026-01-05T08:00:00Z"), EndedAt: ts("2026-01-05T08:00:00Z")},
	}
	raw := `{"type":"user","origin":{"kind":"human"}}`
	evs := []model.Event{
		{ID: "claude:m1-1", SessionID: "claude:m1", TS: ts("2026-01-05T08:00:00Z"), Kind: model.KindMessage, Role: "user", Actor: model.ActorHuman, Text: "a", Raw: raw},
		{ID: "claude:m1-a", SessionID: "claude:m1", TS: ts("2026-01-05T08:00:05Z"), Kind: model.KindMessage, Role: "assistant", Actor: model.ActorAgent, Text: "b"},
		{ID: "claude:m1-t", SessionID: "claude:m1", TS: ts("2026-01-05T08:00:06Z"), Kind: model.KindToolResult, Role: "tool", Actor: model.ActorTool, Text: "t"},
		{ID: "claude:m1-2", SessionID: "claude:m1", TS: ts("2026-01-05T09:00:00Z"), Kind: model.KindMessage, Role: "user", Actor: model.ActorHuman, Text: "# Task: c", RawTruncated: true},
		{ID: "claude:y-1", SessionID: "claude:agent-y", TS: ts("2026-01-05T08:10:00Z"), Kind: model.KindMessage, Role: "user", Actor: model.ActorPeer, Text: "task", Raw: raw},
		{ID: "other:o1-1", SessionID: "other:o1", TS: ts("2026-01-05T08:00:00Z"), Kind: model.KindMessage, Role: "user", Actor: model.ActorHuman, Text: "x"},
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/m.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func TestUserMessages(t *testing.T) {
	s, _ := openTestStore(t)
	seedUserMessages(t, s)
	var got []UserMessage
	if err := s.UserMessages(ctx, "claude", func(m UserMessage) error { got = append(got, m); return nil }); err != nil {
		t.Fatal(err)
	}
	want := []UserMessage{
		{ID: "claude:m1-1", SessionID: "claude:m1", TS: ts("2026-01-05T08:00:00Z"), Actor: model.ActorHuman, Text: "a", Cwd: "/w/m1-dir", GitBranch: "feat/m1", Raw: `{"type":"user","origin":{"kind":"human"}}`},
		{ID: "claude:m1-2", SessionID: "claude:m1", TS: ts("2026-01-05T09:00:00Z"), Actor: model.ActorHuman, Text: "# Task: c", Cwd: "/w/m1-dir", GitBranch: "feat/m1", EarlierPrompt: true},
		{ID: "claude:y-1", SessionID: "claude:agent-y", TS: ts("2026-01-05T08:10:00Z"), Actor: model.ActorPeer, Text: "task", Raw: `{"type":"user","origin":{"kind":"human"}}`, Subagent: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if err := s.UserMessages(ctx, "none", func(UserMessage) error { t.Error("unexpected call"); return nil }); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := s.UserMessages(ctx, "claude", func(UserMessage) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("callback error = %v, want %v", err, boom)
	}
}

func TestSetEventActors(t *testing.T) {
	s, _ := openTestStore(t)
	seedUserMessages(t, s)
	n, err := s.SetEventActors(ctx, []ActorChange{
		{ID: "claude:m1-2", Actor: model.ActorPeer},
		{ID: "claude:m1-1", Actor: model.ActorHuman}, // unchanged
		{ID: "claude:missing", Actor: model.ActorPeer},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("changed = %d, want 1", n)
	}
	_, rows, err := s.Query(ctx, "SELECT actor FROM events WHERE id = 'claude:m1-2'")
	if err != nil || len(rows) != 1 || rows[0][0] != "peer" {
		t.Errorf("actor after update = %v, %v", rows, err)
	}
	if n, err := s.SetEventActors(ctx, nil); err != nil || n != 0 {
		t.Errorf("empty change set = %d, %v", n, err)
	}
	if _, err := s.SetEventActors(ctx, []ActorChange{{ID: "claude:m1-1", Actor: "nobody"}}); err == nil {
		t.Error("invalid actor accepted")
	}
	ro, err := OpenReadOnly(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if _, err := ro.SetEventActors(ctx, []ActorChange{{ID: "claude:m1-1", Actor: model.ActorPeer}}); err == nil {
		t.Error("read-only store accepted update")
	}
}

func TestUserMessagesBetween(t *testing.T) {
	s, _ := openTestStore(t)
	seedUserMessages(t, s)
	for _, tc := range []struct {
		name     string
		from, to string
		want     []string
	}{
		{"open", "", "", []string{"claude:m1-1", "claude:m1-2", "claude:y-1"}},
		{"from", "2026-01-05T08:30:00Z", "", []string{"claude:m1-2"}},
		{"to", "", "2026-01-05T08:10:00Z", []string{"claude:m1-1", "claude:y-1"}},
		{"both ends are included", "2026-01-05T08:10:00Z", "2026-01-05T09:00:00Z", []string{"claude:m1-2", "claude:y-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var from, to time.Time
			if tc.from != "" {
				from = ts(tc.from)
			}
			if tc.to != "" {
				to = ts(tc.to)
			}
			var got []string
			if err := s.UserMessagesBetween(ctx, "claude", from, to, func(m UserMessage) error { got = append(got, m.ID); return nil }); err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestToolCalls(t *testing.T) {
	s, _ := openTestStore(t)
	sessions := []model.Session{{ID: "claude:a", Harness: "claude", NativeID: "a", StartedAt: ts("2026-01-05T08:00:00Z"), EndedAt: ts("2026-01-05T09:00:00Z")}}
	evs := []model.Event{
		{ID: "claude:a-1", SessionID: "claude:a", TS: ts("2026-01-05T08:00:00Z"), Kind: model.KindToolUse, Role: "assistant", Actor: model.ActorAgent, ToolName: "Bash", Text: "ls"},
		{ID: "claude:a-2", SessionID: "claude:a", TS: ts("2026-01-05T08:30:00Z"), Kind: model.KindToolUse, Role: "assistant", Actor: model.ActorAgent, ToolName: "Bash", Text: "herdr agent prompt w1 \"x\""},
		{ID: "claude:a-3", SessionID: "claude:a", TS: ts("2026-01-05T08:30:01Z"), Kind: model.KindToolResult, Role: "tool", Actor: model.ActorTool, Text: "sent"},
		{ID: "claude:a-4", SessionID: "claude:a", TS: ts("2026-01-05T08:31:00Z"), Kind: model.KindMessage, Role: "user", Actor: model.ActorHuman, Text: "hello"},
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
	all, err := s.ToolCalls(ctx, time.Time{}, time.Time{})
	if err != nil || len(all) != 2 || all[0].Text != "ls" || all[1].SessionID != "claude:a" || !all[1].TS.Equal(ts("2026-01-05T08:30:00Z")) {
		t.Fatalf("all calls = %+v, %v", all, err)
	}
	late, err := s.ToolCalls(ctx, ts("2026-01-05T08:10:00Z"), ts("2026-01-05T08:30:00Z"))
	if err != nil || len(late) != 1 || late[0].Text != `herdr agent prompt w1 "x"` {
		t.Errorf("calls in the range = %+v, %v", late, err)
	}
}
