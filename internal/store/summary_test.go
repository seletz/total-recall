package store

import (
	"testing"
	"time"
)

func TestSummarize(t *testing.T) {
	mon := berlinDay(t, 2026, 1, 5)
	dayStart := 4 * time.Hour
	tests := []struct {
		name string
		days []Workday
		want Summary
	}{
		{"no days", nil, Summary{}},
		{"inactive only", []Workday{{Day: mon}, {Day: mon.AddDate(0, 0, 1)}}, Summary{}},
		{
			// 09:00–17:00 → offsets 5h and 13h, span 8h.
			name: "one day",
			days: []Workday{workdayAt(mon, 9*time.Hour, 17*time.Hour)},
			want: Summary{
				First:  Figure{5 * time.Hour, 5 * time.Hour, 5 * time.Hour, 5 * time.Hour},
				Last:   Figure{13 * time.Hour, 13 * time.Hour, 13 * time.Hour, 13 * time.Hour},
				Span:   Figure{8 * time.Hour, 8 * time.Hour, 8 * time.Hour, 8 * time.Hour},
				Active: 1,
			},
		},
		{
			// Firsts 08:00, 09:00, 11:00 (even count after the inactive day
			// is skipped: median is the mean of the middle two), lasts
			// 16:00, 01:30 (+1), 12:00, spans 8h, 16h30m, 1h.
			name: "three days with a gap and a night",
			days: []Workday{
				workdayAt(mon, 8*time.Hour, 16*time.Hour),
				{Day: mon.AddDate(0, 0, 1)},
				workdayAt(mon.AddDate(0, 0, 2), 9*time.Hour, 25*time.Hour+30*time.Minute),
				workdayAt(mon.AddDate(0, 0, 3), 11*time.Hour, 12*time.Hour),
			},
			want: Summary{
				First:  Figure{Min: 4 * time.Hour, Median: 5 * time.Hour, Mean: 16 * time.Hour / 3, Max: 7 * time.Hour},
				Last:   Figure{Min: 8 * time.Hour, Median: 12 * time.Hour, Mean: (12*time.Hour + 21*time.Hour + 30*time.Minute + 8*time.Hour) / 3, Max: 21*time.Hour + 30*time.Minute},
				Span:   Figure{Min: time.Hour, Median: 8 * time.Hour, Mean: (25*time.Hour + 30*time.Minute) / 3, Max: 16*time.Hour + 30*time.Minute},
				Active: 3,
			},
		},
		{
			name: "even count takes the middle",
			days: []Workday{
				workdayAt(mon, 8*time.Hour, 9*time.Hour),
				workdayAt(mon.AddDate(0, 0, 1), 10*time.Hour, 13*time.Hour),
			},
			want: Summary{
				First:  Figure{Min: 4 * time.Hour, Median: 5 * time.Hour, Mean: 5 * time.Hour, Max: 6 * time.Hour},
				Last:   Figure{Min: 5 * time.Hour, Median: 7 * time.Hour, Mean: 7 * time.Hour, Max: 9 * time.Hour},
				Span:   Figure{Min: time.Hour, Median: 2 * time.Hour, Mean: 2 * time.Hour, Max: 3 * time.Hour},
				Active: 2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Summarize(tt.days, dayStart); got != tt.want {
				t.Errorf("Summarize =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

// TestClockOffset checks the wall-clock offsets across a daylight saving
// change: on 2026-03-29 the clock jumps from 02:00 to 03:00, the offset of
// 12:00 CEST is still 8h after a 04:00 boundary.
func TestClockOffset(t *testing.T) {
	loc := berlin(t)
	dst := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
	mon := berlinDay(t, 2026, 1, 5)
	tests := []struct {
		name     string
		t, day   time.Time
		dayStart time.Duration
		want     time.Duration
		clock    string
	}{
		{"morning", mon.Add(9*time.Hour + 15*time.Minute), mon, 4 * time.Hour, 5*time.Hour + 15*time.Minute, "09:15"},
		{"at the boundary", mon.Add(4 * time.Hour), mon, 4 * time.Hour, 0, "04:00"},
		{"past midnight", mon.Add(25*time.Hour + 30*time.Minute), mon, 4 * time.Hour, 21*time.Hour + 30*time.Minute, "01:30 (+1)"},
		{"midnight boundary", mon.Add(25*time.Hour + 30*time.Minute), mon.AddDate(0, 0, 1), 0, time.Hour + 30*time.Minute, "01:30"},
		{"two days on", mon.Add(50 * time.Hour), mon, 4 * time.Hour, 46 * time.Hour, "02:00 (+2)"},
		{"after the DST change", time.Date(2026, 3, 29, 12, 0, 0, 0, loc), dst, 4 * time.Hour, 8 * time.Hour, "12:00"},
		{"utc instant", time.Date(2026, 1, 5, 7, 0, 0, 0, time.UTC), mon, 4 * time.Hour, 4 * time.Hour, "08:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClockOffset(tt.t, tt.day, tt.dayStart)
			if got != tt.want {
				t.Errorf("ClockOffset = %s, want %s", got, tt.want)
			}
			if s := FormatClock(got, tt.dayStart); s != tt.clock {
				t.Errorf("FormatClock = %q, want %q", s, tt.clock)
			}
		})
	}
}
