package store

import (
	"strings"
	"testing"
	"time"
)

func TestTimeline(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	tests := []struct {
		name string
		f    TimelineFilter
		want string // event IDs, oldest first
	}{
		{"all actors, messages only", TimelineFilter{}, "claude:p2-1,claude:p2-2,other:p3-1,other:p3-2,other:p3-3,other:p3-4,other:p3-5,claude:p1-1,claude:p1s-1,claude:p1-2"},
		{"human", TimelineFilter{Actor: "human"}, "claude:p2-1,claude:p2-2,other:p3-1,claude:p1-2"},
		{"every kind", TimelineFilter{Actor: "agent", AllKinds: true}, "other:p3-2,other:p3-3,other:p3-4,other:p3-5,claude:p1-1,claude:p1s-1,claude:p1s-2"},
		{"tool results only with all kinds", TimelineFilter{Actor: "tool"}, ""},
		{"since is inclusive, until exclusive", TimelineFilter{Actor: "human", SessionFilter: SessionFilter{
			Since: ts("2025-12-31T12:00:00Z"), Until: ts("2026-10-31T23:30:00Z")}}, "claude:p2-2,other:p3-1"},
		{"repo", TimelineFilter{Actor: "human", SessionFilter: SessionFilter{Repo: "/w/tool"}}, "other:p3-1"},
		{"project keys take the parent's key for subagents", TimelineFilter{ProjectKeys: []string{"/w/app"}}, "claude:p1-1,claude:p1s-1,claude:p1-2"},
		{"limit keeps the oldest", TimelineFilter{Actor: "human", SessionFilter: SessionFilter{Limit: 2}}, "claude:p2-1,claude:p2-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Timeline(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, e := range got {
				ids = append(ids, e.Event.ID)
			}
			if g := strings.Join(ids, ","); g != tt.want {
				t.Errorf("events = %s, want %s", g, tt.want)
			}
		})
	}
}

func TestTimelineFields(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	got, err := s.Timeline(ctx, TimelineFilter{Actor: "human", SessionFilter: SessionFilter{Repo: "/w/tool"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	e := got[0]
	if e.Event.Text != "g" || !e.Event.TS.Equal(time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)) || e.Event.SessionID != "other:p3" ||
		e.Repo != "/w/tool" || e.Project.Key != "/w/tool" || e.SessionName != "p3" {
		t.Errorf("event = %+v", e)
	}
}
