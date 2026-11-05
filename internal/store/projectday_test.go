package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
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
