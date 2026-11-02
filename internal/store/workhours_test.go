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
	if b := none.Balance([]Workday{d}); b.Target != 0 || b.Span != 8*time.Hour || b.Balance != 8*time.Hour || b.Active != 1 || b.Workdays != 0 {
		t.Errorf("zero hours: Balance = %+v", b)
	}
}

func TestWorkHoursBalance(t *testing.T) {
	mon := berlinDay(t, 2026, 1, 5)
	days := []Workday{
		workdayAt(mon, 9*time.Hour, 18*time.Hour),                                  // Mon 9h: +1h
		{Day: mon.AddDate(0, 0, 1)},                                                // Tue off: no target
		workdayAt(mon.AddDate(0, 0, 2), 10*time.Hour, 15*time.Hour),                // Wed 5h: −3h
		workdayAt(mon.AddDate(0, 0, 5), 11*time.Hour, 12*time.Hour),                // Sat 1h: +1h, no target
		workdayAt(mon.AddDate(0, 0, 6), 23*time.Hour, 24*time.Hour+30*time.Minute), // Sun 1h30m: +1h30m
		workdayAt(mon.AddDate(0, 0, 7), 8*time.Hour, 8*time.Hour+5*time.Second),    // Mon 5s: −7h59m55s
	}
	b := defaultHours.Balance(days)
	want := Balance{
		Span:     16*time.Hour + 30*time.Minute + 5*time.Second,
		Target:   24 * time.Hour,
		Balance:  -(7*time.Hour + 29*time.Minute + 55*time.Second),
		Active:   5,
		Workdays: 3,
	}
	if b != want {
		t.Errorf("Balance = %+v, want %+v", b, want)
	}
	if got := defaultHours.RateBalance(b); got != RatingUnder {
		t.Errorf("RateBalance = %v, want under", got)
	}
	// The balance tolerance scales with the workdays: 3 × (6h−8h) … 3 × (9h−8h).
	tests := []struct {
		balance time.Duration
		want    Rating
	}{
		{-6 * time.Hour, RatingOn},
		{-6*time.Hour - time.Second, RatingUnder},
		{3 * time.Hour, RatingOn},
		{3*time.Hour + time.Second, RatingOver},
		{0, RatingOn},
	}
	for _, tt := range tests {
		if got := defaultHours.RateBalance(Balance{Balance: tt.balance, Workdays: 3}); got != tt.want {
			t.Errorf("RateBalance(%s) = %v, want %v", tt.balance, got, tt.want)
		}
	}
	if got := defaultHours.RateBalance(Balance{}); got != RatingNone {
		t.Errorf("RateBalance(empty) = %v, want none", got)
	}
	if got := defaultHours.RateBalance(Balance{Balance: 2 * time.Hour, Active: 1}); got != RatingNone {
		t.Errorf("RateBalance(weekend only) = %v, want none", got)
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
