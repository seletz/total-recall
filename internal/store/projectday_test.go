package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// projectStrings formats the projects of a day as "key:events[dir]" with
// their slots "@offset:events".
func projectStrings(d ProjectDay) string {
	var out []string
	for _, p := range d.Projects {
		s := fmt.Sprintf("%s:%d", p.Key, p.Events)
		if p.Dir {
			s += "[dir]"
		}
		for _, sl := range p.Slots {
			s += fmt.Sprintf("@%s:%d", sl.Offset, sl.Events)
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// TestProjectDays checks the project activity per day of the seedActivity
// store: the project key of a subagent is its parent's, the day is
// attributed under the day rule (04:00 Berlin), slots are per minute, a
// day's projects are ordered by counted events and the days are gap-free.
func TestProjectDays(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	loc := berlin(t)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 4, 0, 0, 0, loc) }
	tests := []struct {
		name         string
		since, until time.Time
		humanOnly    bool
		days         int
		want         map[string]string // day label -> projectStrings
	}{
		{
			name: "one day all events", since: day(2026, 10, 31), until: day(2026, 11, 1), days: 1,
			want: map[string]string{"2026-10-31": "/w/app:4@19h0m0s:1@19h5m0s:1@19h6m0s:1@20h30m0s:1"},
		},
		{
			name: "one day human only", since: day(2026, 10, 31), until: day(2026, 11, 1), humanOnly: true, days: 1,
			want: map[string]string{"2026-10-31": "/w/app:1@20h30m0s:1"},
		},
		{
			name: "window with empty days", since: day(2026, 10, 15), until: day(2026, 11, 1), days: 17,
			want: map[string]string{
				"2026-10-15": "/w/tool:5@8h0m0s:1@8h1m0s:1@8h2m0s:1@8h3m0s:1@8h4m0s:1",
				"2026-10-16": "",
				"2026-10-31": "/w/app:4@19h0m0s:1@19h5m0s:1@19h6m0s:1@20h30m0s:1",
			},
		},
		{
			name: "directory without repository", since: day(2025, 12, 28), until: day(2026, 1, 1), days: 4,
			want: map[string]string{"2025-12-28": "/w/docs:1[dir]@9h0m0s:1", "2025-12-31": "/w/docs:1[dir]@9h0m0s:1"},
		},
		{
			name: "window without activity", since: day(2026, 3, 1), until: day(2026, 3, 3), days: 2,
			want: map[string]string{"2026-03-01": "", "2026-03-02": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ProjectDays(ctx, WorkdayFilter{Since: tt.since, Until: tt.until, Location: loc, DayStart: 4 * time.Hour, HumanOnly: tt.humanOnly})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.days {
				t.Fatalf("%d days, want %d", len(got), tt.days)
			}
			for i, d := range got {
				if want := tt.since.AddDate(0, 0, i); !d.Day.Equal(time.Date(want.Year(), want.Month(), want.Day(), 0, 0, 0, 0, loc)) || d.Label != want.Format("2006-01-02") {
					t.Errorf("day %d = %v %q, want %v", i, d.Day, d.Label, want)
				}
				if want, ok := tt.want[d.Label]; ok && projectStrings(d) != want {
					t.Errorf("projects of %s = %q, want %q", d.Label, projectStrings(d), want)
				}
			}
		})
	}
}

// TestProjectDaysOrder: two projects on one day are ordered by counted
// events, then by key.
func TestProjectDaysOrder(t *testing.T) {
	s, _ := openTestStore(t)
	seedWorkday(t, s)
	loc := berlin(t)
	got, err := s.ProjectDays(ctx, WorkdayFilter{Location: loc, DayStart: 4 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		for i := 1; i < len(d.Projects); i++ {
			a, b := d.Projects[i-1], d.Projects[i]
			if a.Events < b.Events || (a.Events == b.Events && a.Key > b.Key) {
				t.Errorf("%s: %s:%d before %s:%d", d.Label, a.Key, a.Events, b.Key, b.Events)
			}
		}
	}
}

// TestProjectDaysOpenBounds: without bounds the days run from the first to
// the last day with activity.
func TestProjectDaysOpenBounds(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	got, err := s.ProjectDays(ctx, WorkdayFilter{Location: berlin(t), DayStart: 4 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Label != "2025-12-28" || got[len(got)-1].Label != "2026-10-31" {
		t.Errorf("days = %d, first %q, last %q", len(got), got[0].Label, got[len(got)-1].Label)
	}
}

// TestProjectDaysProjectMapper: the keys go through f.Project like the
// project dimension of ActivitySeries: two repositories of one configured
// project merge into one project of the day with their events and slots
// together, a subagent takes its parent's key and remote, a mapped
// directory is no directory any more.
func TestProjectDaysProjectMapper(t *testing.T) {
	s, _ := openTestStore(t)
	sessions := []model.Session{
		{ID: "claude:m1", Harness: "claude", NativeID: "m1", Cwd: "/w/kompass-api", GitRoot: "/w/kompass-api", GitRemoteURL: "https://git.invalid/mara/kompass-api",
			StartedAt: ts("2026-03-01T10:00:00Z"), EndedAt: ts("2026-03-01T10:05:00Z")},
		{ID: "claude:m2", Harness: "claude", NativeID: "m2", Cwd: "/w/kompass-web/x", GitRoot: "/w/kompass-web", GitRemoteURL: "https://git.invalid/mara/kompass-web",
			StartedAt: ts("2026-03-01T11:00:00Z"), EndedAt: ts("2026-03-01T11:05:00Z")},
		{ID: "claude:m2sub", Harness: "claude", NativeID: "m2sub", ParentID: "claude:m2", Cwd: "/w/kompass-web/x",
			StartedAt: ts("2026-03-01T11:01:00Z"), EndedAt: ts("2026-03-01T11:02:00Z")},
		{ID: "claude:m3", Harness: "claude", NativeID: "m3", Cwd: "/w/notes",
			StartedAt: ts("2026-03-01T12:00:00Z"), EndedAt: ts("2026-03-01T12:05:00Z")},
	}
	evs := []model.Event{
		ev("claude:m1-1", "claude:m1", "2026-03-01T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "a"),
		ev("claude:m2-1", "claude:m2", "2026-03-01T11:00:00Z", model.ActorHuman, model.KindMessage, "user", "b"),
		ev("claude:m2s-1", "claude:m2sub", "2026-03-01T11:01:00Z", model.ActorAgent, model.KindMessage, "assistant", "c"),
		ev("claude:m3-1", "claude:m3", "2026-03-01T12:00:00Z", model.ActorHuman, model.KindMessage, "user", "d"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/m.jsonl", Harness: "claude", Offset: 1}, sessions, evs); err != nil {
		t.Fatal(err)
	}
	mapper := func(key string, dir bool, remote string) string {
		switch {
		case strings.HasPrefix(remote, "https://git.invalid/mara/kompass"):
			return "kompass"
		case key == "/w/notes" && dir:
			return "notizbox"
		}
		return key
	}
	tests := []struct {
		name   string
		mapper func(key string, dir bool, remote string) string
		want   string
	}{
		{"without mapper", nil, "/w/kompass-web:2@11h0m0s:1@11h1m0s:1 /w/kompass-api:1@10h0m0s:1 /w/notes:1[dir]@12h0m0s:1"},
		{"with mapper", mapper, "kompass:3@10h0m0s:1@11h0m0s:1@11h1m0s:1 notizbox:1@12h0m0s:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ProjectDays(ctx, WorkdayFilter{Location: time.UTC, Project: tt.mapper})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || projectStrings(got[0]) != tt.want {
				t.Errorf("got %d days, %q; want 1 day, %q", len(got), projectStrings(got[0]), tt.want)
			}
		})
	}
}

// TestProjectDaysEmptyStore: an empty archive yields no days.
func TestProjectDaysEmptyStore(t *testing.T) {
	s, _ := openTestStore(t)
	got, err := s.ProjectDays(ctx, WorkdayFilter{DayStart: 4 * time.Hour})
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestProjectDaysRejectsBadDayStart(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.ProjectDays(ctx, WorkdayFilter{DayStart: 24 * time.Hour}); err == nil {
		t.Error("no error for a day start of 24h")
	}
}
