package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// slotStrings formats the slots of a day as "key@offset:events".
func slotStrings(keys []string, d ProjectDay) string {
	var out []string
	for _, s := range d.Slots {
		out = append(out, fmt.Sprintf("%s@%s:%d", keys[s.Project], s.Offset, s.Events))
	}
	return strings.Join(out, " ")
}

// TestProjectDays checks the project activity per day of the seedActivity
// store: the project key of a subagent is its parent's, the day is
// attributed under the day rule (04:00 Berlin), slots are per minute and
// the keys are ordered by counted events over the window.
func TestProjectDays(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	loc := berlin(t)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 4, 0, 0, 0, loc) }
	tests := []struct {
		name         string
		since, until time.Time
		humanOnly    bool
		keys         []string
		events       []int
		dirs         []string
		days         int
		slots        map[string]string // day label -> slotStrings
	}{
		{
			name: "one day all events", since: day(2026, 10, 31), until: day(2026, 11, 1),
			keys: []string{"/w/app"}, events: []int{4}, days: 1,
			slots: map[string]string{"2026-10-31": "/w/app@19h0m0s:1 /w/app@19h5m0s:1 /w/app@19h6m0s:1 /w/app@20h30m0s:1"},
		},
		{
			name: "one day human only", since: day(2026, 10, 31), until: day(2026, 11, 1), humanOnly: true,
			keys: []string{"/w/app"}, events: []int{1}, days: 1,
			slots: map[string]string{"2026-10-31": "/w/app@20h30m0s:1"},
		},
		{
			name: "window orders keys by volume", since: day(2026, 10, 15), until: day(2026, 11, 1),
			keys: []string{"/w/tool", "/w/app"}, events: []int{5, 4}, days: 17,
			slots: map[string]string{
				"2026-10-15": "/w/tool@8h0m0s:1 /w/tool@8h1m0s:1 /w/tool@8h2m0s:1 /w/tool@8h3m0s:1 /w/tool@8h4m0s:1",
				"2026-10-16": "",
				"2026-10-31": "/w/app@19h0m0s:1 /w/app@19h5m0s:1 /w/app@19h6m0s:1 /w/app@20h30m0s:1",
			},
		},
		{
			name: "directory without repository", since: day(2025, 12, 28), until: day(2026, 1, 1),
			keys: []string{"/w/docs"}, events: []int{2}, dirs: []string{"/w/docs"}, days: 4,
			slots: map[string]string{"2025-12-28": "/w/docs@9h0m0s:1", "2025-12-31": "/w/docs@9h0m0s:1"},
		},
		{
			name: "window without activity", since: day(2026, 3, 1), until: day(2026, 3, 3),
			days: 2, slots: map[string]string{"2026-03-01": "", "2026-03-02": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ProjectDays(ctx, WorkdayFilter{Since: tt.since, Until: tt.until, Location: loc, DayStart: 4 * time.Hour, HumanOnly: tt.humanOnly})
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(got.Keys) != fmt.Sprint(tt.keys) || fmt.Sprint(got.Events) != fmt.Sprint(tt.events) {
				t.Errorf("keys = %v %v, want %v %v", got.Keys, got.Events, tt.keys, tt.events)
			}
			if fmt.Sprint(got.Dirs) != fmt.Sprint(tt.dirs) {
				t.Errorf("dirs = %v, want %v", got.Dirs, tt.dirs)
			}
			if len(got.Days) != tt.days {
				t.Fatalf("%d days, want %d", len(got.Days), tt.days)
			}
			for i, d := range got.Days {
				if want := tt.since.AddDate(0, 0, i); !d.Day.Equal(time.Date(want.Year(), want.Month(), want.Day(), 0, 0, 0, 0, loc)) {
					t.Errorf("day %d = %v, want %v", i, d.Day, want)
				}
				if want, ok := tt.slots[d.Day.Format("2006-01-02")]; ok && slotStrings(got.Keys, d) != want {
					t.Errorf("slots of %s = %q, want %q", d.Day.Format("2006-01-02"), slotStrings(got.Keys, d), want)
				}
			}
		})
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
	if len(got.Days) == 0 || got.Days[0].Label != "2025-12-28" || got.Days[len(got.Days)-1].Label != "2026-10-31" {
		t.Errorf("days = %d, first %q, last %q", len(got.Days), got.Days[0].Label, got.Days[len(got.Days)-1].Label)
	}
	if want := []string{"/w/tool", "/w/app", "/w/docs"}; fmt.Sprint(got.Keys) != fmt.Sprint(want) {
		t.Errorf("keys = %v, want %v", got.Keys, want)
	}
}

// TestProjectDaysEmptyStore: an empty archive yields no days.
func TestProjectDaysEmptyStore(t *testing.T) {
	s, _ := openTestStore(t)
	got, err := s.ProjectDays(ctx, WorkdayFilter{DayStart: 4 * time.Hour})
	if err != nil || len(got.Days) != 0 || len(got.Keys) != 0 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestProjectDaysRejectsBadDayStart(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.ProjectDays(ctx, WorkdayFilter{DayStart: 24 * time.Hour}); err == nil {
		t.Error("no error for a day start of 24h")
	}
}
