package store

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

func TestProjectKeys(t *testing.T) {
	s, _ := openTestStore(t)
	sessions := []model.Session{
		{ID: "claude:m1", Harness: "claude", NativeID: "m1", Cwd: "/w/kompass", GitRoot: "/w/kompass", GitRemoteURL: "https://git.invalid/mara/kompass",
			StartedAt: ts("2026-03-01T10:00:00Z"), EndedAt: ts("2026-03-01T10:05:00Z")},
		// A worktree of the same repository: same git root, counted with it.
		{ID: "claude:m2", Harness: "claude", NativeID: "m2", Cwd: "/w/kompass__worktrees/gpx", GitRoot: "/w/kompass", GitRemoteURL: "https://git.invalid/mara/kompass",
			StartedAt: ts("2026-03-03T11:00:00Z"), EndedAt: ts("2026-03-03T11:05:00Z")},
		// A subagent: its parent's session counts, not its own.
		{ID: "claude:m2sub", Harness: "claude", NativeID: "m2sub", ParentID: "claude:m2", Cwd: "/w/kompass__worktrees/gpx",
			StartedAt: ts("2026-03-03T11:01:00Z"), EndedAt: ts("2026-03-03T11:02:00Z")},
		// A directory without a repository; the second session has no end.
		{ID: "claude:m3", Harness: "claude", NativeID: "m3", Cwd: "/w/notes", StartedAt: ts("2026-03-02T11:00:00Z"), EndedAt: ts("2026-03-02T11:05:00Z")},
		{ID: "claude:m4", Harness: "claude", NativeID: "m4", Cwd: "/w/notes", StartedAt: ts("2026-03-04T09:00:00Z")},
		// A session without a working directory is not a project.
		{ID: "claude:m5", Harness: "claude", NativeID: "m5", StartedAt: ts("2026-03-04T10:00:00Z")},
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/m.jsonl", Harness: "claude", Offset: 1}, sessions, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.ProjectKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []ProjectKey{
		{Key: "/w/kompass", Remote: "https://git.invalid/mara/kompass", Sessions: 2, Last: ts("2026-03-03T11:05:00Z")},
		{Key: "/w/notes", Dir: true, Sessions: 2, Last: ts("2026-03-04T09:00:00Z")},
	}
	if len(got) != len(want) {
		t.Fatalf("ProjectKeys = %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Key != w.Key || g.Dir != w.Dir || g.Remote != w.Remote || g.Sessions != w.Sessions || !g.Last.Equal(w.Last) {
			t.Errorf("ProjectKeys[%d] = %+v, want %+v", i, g, w)
		}
	}
	// An empty archive has no keys.
	e, _ := openTestStore(t)
	if keys, err := e.ProjectKeys(ctx); err != nil || len(keys) != 0 {
		t.Errorf("empty: %v, %v", keys, err)
	}
}
