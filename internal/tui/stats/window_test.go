package stats

import (
	"testing"
	"time"
	_ "time/tzdata" // the window tests pin Europe/Berlin boundaries

	"github.com/seletz/total-recall/internal/store"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// date is a local midnight in loc.
func date(loc *time.Location, y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func TestDefaultCount(t *testing.T) {
	tests := []struct {
		p    store.Period
		want int
	}{
		{store.PeriodDay, 28},
		{store.PeriodWeek, 16},
		{store.PeriodMonth, 12},
	}
	for _, tt := range tests {
		if got := DefaultCount(tt.p); got != tt.want {
			t.Errorf("DefaultCount(%s) = %d, want %d", tt.p, got, tt.want)
		}
	}
}

// TestNewWindow checks the default windows end with the period containing
// now and start whole periods earlier, at local midnight in Europe/Berlin.
func TestNewWindow(t *testing.T) {
	loc := berlin(t)
	// A Sunday noon in winter (UTC+1); the UTC instant is the same day.
	sunday := time.Date(2026, 1, 11, 11, 0, 0, 0, time.UTC)
	// 23:30 UTC on a Wednesday is already Thursday in Berlin.
	lateUTC := time.Date(2025, 12, 31, 23, 30, 0, 0, time.UTC)
	// A day after the switch to summer time (2026-03-29).
	spring := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		p            store.Period
		now          time.Time
		since, until time.Time
		label        string
	}{
		{"day", store.PeriodDay, sunday, date(loc, 2025, 12, 15), date(loc, 2026, 1, 12), "2025-12-15 – 2026-01-11"},
		{"week", store.PeriodWeek, sunday, date(loc, 2025, 9, 22), date(loc, 2026, 1, 12), "2025-09-22 – 2026-01-11"},
		{"month", store.PeriodMonth, sunday, date(loc, 2025, 2, 1), date(loc, 2026, 2, 1), "2025-02-01 – 2026-01-31"},
		{"day crosses midnight in Berlin", store.PeriodDay, lateUTC, date(loc, 2025, 12, 5), date(loc, 2026, 1, 2), "2025-12-05 – 2026-01-01"},
		{"week of new year", store.PeriodWeek, lateUTC, date(loc, 2025, 9, 15), date(loc, 2026, 1, 5), "2025-09-15 – 2026-01-04"},
		{"month of new year", store.PeriodMonth, lateUTC, date(loc, 2025, 2, 1), date(loc, 2026, 2, 1), "2025-02-01 – 2026-01-31"},
		{"day across DST", store.PeriodDay, spring, date(loc, 2026, 3, 9), date(loc, 2026, 4, 6), "2026-03-09 – 2026-04-05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWindow(tt.p, tt.now, loc)
			if w.Count != DefaultCount(tt.p) {
				t.Errorf("Count = %d, want %d", w.Count, DefaultCount(tt.p))
			}
			if got := w.Since(); !got.Equal(tt.since) || got.Location() != loc {
				t.Errorf("Since() = %v, want %v", got, tt.since)
			}
			if got := w.Until(); !got.Equal(tt.until) || got.Location() != loc {
				t.Errorf("Until() = %v, want %v", got, tt.until)
			}
			if got := w.String(); got != tt.label {
				t.Errorf("String() = %q, want %q", got, tt.label)
			}
		})
	}
}

func TestWindowShiftZoom(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 1, 11, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		w            Window
		count        int
		since, until time.Time
	}{
		{"day back", NewWindow(store.PeriodDay, now, loc).Shift(-1), 28, date(loc, 2025, 11, 17), date(loc, 2025, 12, 15)},
		{"day back and forth", NewWindow(store.PeriodDay, now, loc).Shift(-1).Shift(1), 28, date(loc, 2025, 12, 15), date(loc, 2026, 1, 12)},
		{"day forward", NewWindow(store.PeriodDay, now, loc).Shift(1), 28, date(loc, 2026, 1, 12), date(loc, 2026, 2, 9)},
		{"week back", NewWindow(store.PeriodWeek, now, loc).Shift(-1), 16, date(loc, 2025, 6, 2), date(loc, 2025, 9, 22)},
		{"month back", NewWindow(store.PeriodMonth, now, loc).Shift(-1), 12, date(loc, 2024, 2, 1), date(loc, 2025, 2, 1)},
		{"day zoom out keeps the end", NewWindow(store.PeriodDay, now, loc).Zoom(-1), 14, date(loc, 2025, 12, 29), date(loc, 2026, 1, 12)},
		{"day zoom in", NewWindow(store.PeriodDay, now, loc).Zoom(1), 56, date(loc, 2025, 11, 17), date(loc, 2026, 1, 12)},
		{"zoom out stops at one period", NewWindow(store.PeriodMonth, now, loc).Zoom(-9), 1, date(loc, 2026, 1, 1), date(loc, 2026, 2, 1)},
		{"zoom in stops at eight windows", NewWindow(store.PeriodWeek, now, loc).Zoom(9), 128, date(loc, 2023, 7, 31), date(loc, 2026, 1, 12)},
		{"shift after zoom moves by the zoomed window", NewWindow(store.PeriodDay, now, loc).Zoom(-1).Shift(-1), 14, date(loc, 2025, 12, 15), date(loc, 2025, 12, 29)},
		{"month zoom then shift lands on month starts", NewWindow(store.PeriodMonth, now, loc).Zoom(-2).Shift(-1), 3, date(loc, 2025, 8, 1), date(loc, 2025, 11, 1)},
		{"month shift clamps the day of the month", NewWindow(store.PeriodMonth, date(loc, 2026, 3, 31), loc).Zoom(-9).Shift(-1).Shift(1), 1, date(loc, 2026, 3, 1), date(loc, 2026, 4, 1)},
		{"month shift from the 31st does not drift", NewWindow(store.PeriodMonth, date(loc, 2026, 1, 31), loc).Zoom(-9).Shift(1), 1, date(loc, 2026, 2, 1), date(loc, 2026, 3, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.w.Count != tt.count {
				t.Errorf("Count = %d, want %d", tt.w.Count, tt.count)
			}
			if got := tt.w.Since(); !got.Equal(tt.since) {
				t.Errorf("Since() = %v, want %v", got, tt.since)
			}
			if got := tt.w.Until(); !got.Equal(tt.until) {
				t.Errorf("Until() = %v, want %v", got, tt.until)
			}
		})
	}
}

// TestWindowWithPeriod checks that switching the period keeps the end of
// the window and takes the default count of the new period.
func TestWindowWithPeriod(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 1, 11, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		w            Window
		count        int
		since, until time.Time
	}{
		{"day to week", NewWindow(store.PeriodDay, now, loc).WithPeriod(store.PeriodWeek), 16, date(loc, 2025, 9, 22), date(loc, 2026, 1, 12)},
		{"day to month", NewWindow(store.PeriodDay, now, loc).WithPeriod(store.PeriodMonth), 12, date(loc, 2025, 2, 1), date(loc, 2026, 2, 1)},
		{"shifted day to week", NewWindow(store.PeriodDay, now, loc).Shift(-1).WithPeriod(store.PeriodWeek), 16, date(loc, 2025, 8, 25), date(loc, 2025, 12, 15)},
		{"month to day ends today", NewWindow(store.PeriodMonth, now, loc).WithPeriod(store.PeriodDay), 28, date(loc, 2025, 12, 15), date(loc, 2026, 1, 12)},
		{"day to week to day returns", NewWindow(store.PeriodDay, now, loc).WithPeriod(store.PeriodWeek).WithPeriod(store.PeriodDay), 28, date(loc, 2025, 12, 15), date(loc, 2026, 1, 12)},
		{"zoom does not survive the switch", NewWindow(store.PeriodDay, now, loc).Zoom(1).WithPeriod(store.PeriodDay), 28, date(loc, 2025, 12, 15), date(loc, 2026, 1, 12)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.w.Count != tt.count {
				t.Errorf("Count = %d, want %d", tt.w.Count, tt.count)
			}
			if got := tt.w.Since(); !got.Equal(tt.since) {
				t.Errorf("Since() = %v, want %v", got, tt.since)
			}
			if got := tt.w.Until(); !got.Equal(tt.until) {
				t.Errorf("Until() = %v, want %v", got, tt.until)
			}
		})
	}
}
