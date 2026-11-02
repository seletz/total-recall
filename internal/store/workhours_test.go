package store

import (
	"testing"
	"time"
)

// berlinDay returns local midnight of the date in Europe/Berlin.
func berlinDay(t *testing.T, y int, m time.Month, d int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// defaultHours is the built-in working time: 09:00–17:00, 8h, 6h–9h on target.
var defaultHours = WorkHours{Start: 9 * time.Hour, End: 17 * time.Hour, Target: 8 * time.Hour, Low: 6 * time.Hour, High: 9 * time.Hour}

// workdayAt builds an active day: first and last as local clock offsets
// from the day's midnight (26h = 02:00 the next day).
func workdayAt(day time.Time, first, last time.Duration) Workday {
	return Workday{Day: day, Label: day.Format("2006-01-02"), First: day.Add(first), Last: day.Add(last)}
}

// holidayAt builds an active day on the named public holiday.
func holidayAt(day time.Time, name string, first, last time.Duration) Workday {
	d := workdayAt(day, first, last)
	d.Holiday = name
	return d
}

func TestWorkdayOff(t *testing.T) {
	mon := berlinDay(t, 2026, 1, 5)
	sat := berlinDay(t, 2026, 1, 10)
	tests := []struct {
		name           string
		day            Workday
		holiday, isOff bool
	}{
		{"weekday", Workday{Day: mon}, false, false},
		{"weekend", Workday{Day: sat}, false, true},
		{"holiday", Workday{Day: mon, Holiday: "Neujahr"}, true, true},
		{"holiday on a weekend", Workday{Day: sat, Holiday: "Neujahr"}, true, true},
	}
	for _, tt := range tests {
		if got := tt.day.IsHoliday(); got != tt.holiday {
			t.Errorf("%s: IsHoliday = %v, want %v", tt.name, got, tt.holiday)
		}
		if got := tt.day.IsOff(); got != tt.isOff {
			t.Errorf("%s: IsOff = %v, want %v", tt.name, got, tt.isOff)
		}
	}
}

func TestIsWeekend(t *testing.T) {
	// 2026-01-05 is a Monday.
	for d, want := range map[int]bool{5: false, 9: false, 10: true, 11: true, 12: false} {
		if got := IsWeekend(berlinDay(t, 2026, 1, d)); got != want {
			t.Errorf("IsWeekend(2026-01-%02d) = %v, want %v", d, got, want)
		}
	}
}

func TestWorkHoursCore(t *testing.T) {
	// The DST change on 2026-03-29 moves the clock forward at 02:00; the
	// core hours keep their wall-clock times.
	day := berlinDay(t, 2026, 3, 29)
	start, end := defaultHours.Core(day)
	if start.Format("15:04 MST") != "09:00 CEST" || end.Format("15:04 MST") != "17:00 CEST" {
		t.Errorf("Core = %s – %s", start, end)
	}
	if end.Sub(start) != 8*time.Hour {
		t.Errorf("core length = %s, want 8h", end.Sub(start))
	}
}

func TestWorkHoursDiffAndRating(t *testing.T) {
	mon := berlinDay(t, 2026, 1, 5)
	sat := berlinDay(t, 2026, 1, 10)
	tests := []struct {
		name   string
		day    Workday
		target time.Duration
		diff   time.Duration
		ok     bool
		rating Rating
	}{
		{"inactive weekday", Workday{Day: mon}, 0, 0, false, RatingNone},
		{"inactive weekend", Workday{Day: sat}, 0, 0, false, RatingNone},
		{"on target", workdayAt(mon, 9*time.Hour, 17*time.Hour+30*time.Minute), 8 * time.Hour, 30 * time.Minute, true, RatingOn},
		{"exactly low", workdayAt(mon, 9*time.Hour, 15*time.Hour), 8 * time.Hour, -2 * time.Hour, true, RatingOn},
		{"under", workdayAt(mon, 9*time.Hour, 14*time.Hour+59*time.Minute), 8 * time.Hour, -(2*time.Hour + 1*time.Minute), true, RatingUnder},
		{"exactly high", workdayAt(mon, 8*time.Hour, 17*time.Hour), 8 * time.Hour, time.Hour, true, RatingOn},
		{"over", workdayAt(mon, 8*time.Hour, 17*time.Hour+1*time.Second), 8 * time.Hour, time.Hour + time.Second, true, RatingOver},
		{"seconds only", workdayAt(mon, 15*time.Hour, 15*time.Hour+5*time.Second), 8 * time.Hour, -(8*time.Hour - 5*time.Second), true, RatingUnder},
		{"weekend has no target", workdayAt(sat, 11*time.Hour, 13*time.Hour), 0, 2 * time.Hour, true, RatingNone},
		{"holiday has no target", holidayAt(mon.AddDate(0, 0, 1), "Epiphanias", 9*time.Hour, 12*time.Hour), 0, 3 * time.Hour, true, RatingNone},
		{"inactive holiday", Workday{Day: mon.AddDate(0, 0, 1), Holiday: "Epiphanias"}, 0, 0, false, RatingNone},
		{"holiday on a weekend", holidayAt(sat, "Tag der deutschen Einheit", 9*time.Hour, 12*time.Hour), 0, 3 * time.Hour, true, RatingNone},
		{"past midnight", workdayAt(mon, 23*time.Hour+40*time.Minute, 24*time.Hour+5*time.Minute), 8 * time.Hour, -(7*time.Hour + 35*time.Minute), true, RatingUnder},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultHours.TargetFor(tt.day); got != tt.target {
				t.Errorf("TargetFor = %s, want %s", got, tt.target)
			}
			diff, ok := defaultHours.Diff(tt.day)
			if diff != tt.diff || ok != tt.ok {
				t.Errorf("Diff = %s, %v; want %s, %v", diff, ok, tt.diff, tt.ok)
			}
			if got := defaultHours.Rate(tt.day); got != tt.rating {
				t.Errorf("Rate = %v, want %v", got, tt.rating)
			}
		})
	}
}

func TestWorkHoursZero(t *testing.T) {
	mon := berlinDay(t, 2026, 1, 5)
	var none WorkHours
	if !none.IsZero() || defaultHours.IsZero() {
		t.Error("IsZero")
	}
	d := workdayAt(mon, 9*time.Hour, 17*time.Hour)
	if diff, ok := none.Diff(d); ok || diff != 0 {
		t.Errorf("zero hours: Diff = %s, %v", diff, ok)
	}
	if got := none.Rate(d); got != RatingNone {
		t.Errorf("zero hours: Rate = %v", got)
	}
	if b := none.Balance([]Workday{d}, time.Time{}); b.Target != 0 || b.Span != 8*time.Hour || b.Balance != 8*time.Hour || b.Active != 1 || b.Workdays != 0 {
		t.Errorf("zero hours: Balance = %+v", b)
	}
}

// TestWorkHoursBalance checks the sum of a range: every weekday that is
// no holiday is a workday, with or without activity, up to asOf.
func TestWorkHoursBalance(t *testing.T) {
	mon := berlinDay(t, 2026, 1, 5)
	days := []Workday{
		workdayAt(mon, 9*time.Hour, 18*time.Hour),                                  // Mon 9h
		{Day: mon.AddDate(0, 0, 1), Holiday: "Epiphanias"},                         // Tue holiday: no target
		workdayAt(mon.AddDate(0, 0, 2), 10*time.Hour, 15*time.Hour),                // Wed 5h
		{Day: mon.AddDate(0, 0, 3)},                                                // Thu off: still a workday
		holidayAt(mon.AddDate(0, 0, 4), "Brückentag", 9*time.Hour, 11*time.Hour),   // Fri holiday with 2h activity
		workdayAt(mon.AddDate(0, 0, 5), 11*time.Hour, 12*time.Hour),                // Sat 1h, no target
		workdayAt(mon.AddDate(0, 0, 6), 23*time.Hour, 24*time.Hour+30*time.Minute), // Sun 1h30m
		workdayAt(mon.AddDate(0, 0, 7), 8*time.Hour, 8*time.Hour+5*time.Second),    // Mon 5s
		{Day: mon.AddDate(0, 0, 8)},                                                // Tue, future with asOf = second Mon
	}
	tests := []struct {
		name string
		asOf time.Time
		want Balance
	}{
		{
			name: "whole range",
			want: Balance{
				Span:     18*time.Hour + 30*time.Minute + 5*time.Second,
				Target:   40 * time.Hour, // Mon, Wed, Thu, Mon, Tue
				Balance:  -(21*time.Hour + 29*time.Minute + 55*time.Second),
				Active:   6,
				Workdays: 5,
				Holidays: 2,
			},
		},
		{
			name: "as of the second Monday",
			asOf: mon.AddDate(0, 0, 7),
			want: Balance{
				Span:     18*time.Hour + 30*time.Minute + 5*time.Second,
				Target:   32 * time.Hour,
				Balance:  -(13*time.Hour + 29*time.Minute + 55*time.Second),
				Active:   6,
				Workdays: 4,
				Holidays: 2,
			},
		},
		{
			name: "as of before the range",
			asOf: mon.AddDate(0, 0, -1),
			want: Balance{
				Span:     18*time.Hour + 30*time.Minute + 5*time.Second,
				Balance:  18*time.Hour + 30*time.Minute + 5*time.Second,
				Active:   6,
				Holidays: 2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if b := defaultHours.Balance(days, tt.asOf); b != tt.want {
				t.Errorf("Balance = %+v, want %+v", b, tt.want)
			}
		})
	}
}

// TestRateBalance checks the rating of a range: with weekly bounds they
// scale with the workdays (32h–48h for five), without them the daily
// bounds times the workdays apply.
func TestRateBalance(t *testing.T) {
	weekly := defaultHours
	weekly.WeeklyLow, weekly.WeeklyHigh = 32*time.Hour, 48*time.Hour
	tests := []struct {
		name     string
		w        WorkHours
		span     time.Duration
		workdays int
		want     Rating
	}{
		{"full week on", weekly, 40 * time.Hour, 5, RatingOn},
		{"full week at low", weekly, 32 * time.Hour, 5, RatingOn},
		{"full week under", weekly, 32*time.Hour - time.Second, 5, RatingUnder},
		{"full week at high", weekly, 48 * time.Hour, 5, RatingOn},
		{"full week over", weekly, 48*time.Hour + time.Second, 5, RatingOver},
		{"four workdays scale to 25h36m–38h24m", weekly, 25*time.Hour + 36*time.Minute, 4, RatingOn},
		{"four workdays under", weekly, 25*time.Hour + 35*time.Minute, 4, RatingUnder},
		{"four workdays over", weekly, 38*time.Hour + 25*time.Minute, 4, RatingOver},
		{"a month of 20 workdays", weekly, 160 * time.Hour, 20, RatingOn},
		{"a month under", weekly, 127 * time.Hour, 20, RatingUnder},
		{"one workday", weekly, 7 * time.Hour, 1, RatingOn},
		{"no workday", weekly, 7 * time.Hour, 0, RatingNone},
		{"daily bounds: 3 × 6h–9h", defaultHours, 18 * time.Hour, 3, RatingOn},
		{"daily bounds under", defaultHours, 18*time.Hour - time.Second, 3, RatingUnder},
		{"daily bounds over", defaultHours, 27*time.Hour + time.Second, 3, RatingOver},
		{"no working time", WorkHours{}, 40 * time.Hour, 5, RatingNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.RateBalance(Balance{Span: tt.span, Workdays: tt.workdays}); got != tt.want {
				t.Errorf("RateBalance = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatSpan(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{25*time.Minute + 5*time.Second, "25m"},
		{3*time.Hour + 5*time.Minute, "3h05m"},
		{72 * time.Hour, "72h00m"},
	}
	for _, tt := range tests {
		if got := FormatSpan(tt.d); got != tt.want {
			t.Errorf("FormatSpan(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatDiff(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "+0h00m"},
		{30 * time.Second, "+0h00m"},
		{time.Hour + 12*time.Minute, "+1h12m"},
		{-(30 * time.Minute), "-0h30m"},
		{-(7*time.Hour + 59*time.Minute + 55*time.Second), "-7h59m"},
		{26 * time.Hour, "+26h00m"},
	}
	for _, tt := range tests {
		if got := FormatDiff(tt.d); got != tt.want {
			t.Errorf("FormatDiff(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
