package holiday

import (
	"strings"
	"testing"
	"time"
)

var berlin = mustLoad("Europe/Berlin")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func date(y int, m time.Month, d int, loc *time.Location) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// TestName checks fixed dates: Easter-dependent holidays in two years, the
// differences between Bundesländer, holidays that fall on a weekend, and
// days that are none.
func TestName(t *testing.T) {
	tests := []struct {
		name       string
		bundesland string
		day        time.Time
		want       string
	}{
		{"new year", "Baden-Württemberg", date(2026, 1, 1, berlin), "Neujahr"},
		{"epiphany in BW", "Baden-Württemberg", date(2026, 1, 6, berlin), "Epiphanias"},
		{"epiphany in Bayern", "Bayern", date(2026, 1, 6, berlin), "Epiphanias"},
		{"no epiphany in Berlin", "Berlin", date(2026, 1, 6, berlin), ""},
		{"no epiphany nationwide", "Deutschland", date(2026, 1, 6, berlin), ""},
		{"good friday 2026", "Baden-Württemberg", date(2026, 4, 3, berlin), "Karfreitag"},
		{"easter monday 2026", "Baden-Württemberg", date(2026, 4, 6, berlin), "Ostermontag"},
		{"good friday 2024", "Baden-Württemberg", date(2024, 3, 29, berlin), "Karfreitag"},
		{"easter monday 2024", "Baden-Württemberg", date(2024, 4, 1, berlin), "Ostermontag"},
		{"not easter monday in 2024", "Baden-Württemberg", date(2024, 4, 6, berlin), ""},
		{"corpus christi in BW", "Baden-Württemberg", date(2026, 6, 4, berlin), "Fronleichnam"},
		{"no corpus christi in Hamburg", "Hamburg", date(2026, 6, 4, berlin), ""},
		{"reformation day in Hamburg", "Hamburg", date(2026, 10, 31, berlin), "Reformationstag"},
		{"no reformation day in BW", "Baden-Württemberg", date(2026, 10, 31, berlin), ""},
		{"repentance day in Sachsen", "Sachsen", date(2026, 11, 18, berlin), "Buß- und Bettag"},
		{"unity day on a Saturday", "Baden-Württemberg", date(2026, 10, 3, berlin), "Tag der deutschen Einheit"},
		{"all saints on a Sunday", "Baden-Württemberg", date(2026, 11, 1, berlin), "Allerheiligen"},
		{"boxing day on a Saturday", "Deutschland", date(2026, 12, 26, berlin), "Zweiter Weihnachtsfeiertag"},
		{"christmas 2025", "Baden-Württemberg", date(2025, 12, 25, berlin), "Weihnachten"},
		{"ordinary weekday", "Baden-Württemberg", date(2026, 1, 7, berlin), ""},
		{"christmas eve is none", "Baden-Württemberg", date(2026, 12, 24, berlin), ""},
		{"spelling without hyphen", "BadenWürttemberg", date(2026, 1, 6, berlin), "Epiphanias"},
		{"spelling lower case", "baden-württemberg", date(2026, 1, 6, berlin), "Epiphanias"},
		{"unknown falls back to nationwide", "Atlantis", date(2026, 1, 6, berlin), ""},
		{"unknown still has nationwide holidays", "Atlantis", date(2026, 5, 1, berlin), "Tag der Arbeit"},
		{"empty falls back to nationwide", "", date(2026, 1, 6, berlin), ""},
		// The local date counts, not the UTC date: 00:30 CET is 23:30 UTC
		// of the day before, 23:30 CET is 22:30 UTC of the same day.
		{"local date after midnight", "Baden-Württemberg", time.Date(2026, 1, 6, 0, 30, 0, 0, berlin), "Epiphanias"},
		{"local date before midnight", "Baden-Württemberg", time.Date(2026, 1, 5, 23, 30, 0, 0, berlin), ""},
		{"utc instant of a local holiday", "Baden-Württemberg", time.Date(2026, 1, 5, 23, 30, 0, 0, time.UTC), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(tt.bundesland)
			if got := c.Name(tt.day); got != tt.want {
				t.Errorf("Name(%s) = %q, want %q", tt.day, got, tt.want)
			}
			if got := c.Is(tt.day); got != (tt.want != "") {
				t.Errorf("Is(%s) = %v, want %v", tt.day, got, tt.want != "")
			}
		})
	}
}

// TestRegion checks which region a Bundesland name resolves to.
func TestRegion(t *testing.T) {
	tests := []struct {
		bundesland string
		want       string
		known      bool
	}{
		{"Baden-Württemberg", "Baden-Württemberg", true},
		{"BadenWürttemberg", "Baden-Württemberg", true},
		{"nordrhein-westfalen", "Nordrhein-Westfalen", true},
		{"Nordrhein Westfalen", "Nordrhein-Westfalen", true},
		{"Deutschland", "Deutschland", true},
		{"", "Deutschland", false},
		{"Atlantis", "Deutschland", false},
	}
	for _, tt := range tests {
		t.Run(tt.bundesland, func(t *testing.T) {
			c := New(tt.bundesland)
			if got := c.Region(); got != tt.want {
				t.Errorf("Region() = %q, want %q", got, tt.want)
			}
			if got := Known(tt.bundesland); got != tt.known {
				t.Errorf("Known(%q) = %v, want %v", tt.bundesland, got, tt.known)
			}
		})
	}
}

// TestRegions checks the list of names: every one is known and resolves to
// itself, the nationwide region is last.
func TestRegions(t *testing.T) {
	names := Regions()
	if len(names) != 17 {
		t.Fatalf("Regions() has %d entries, want 16 Bundesländer and Deutschland: %v", len(names), names)
	}
	for _, n := range names {
		if !Known(n) || New(n).Region() != n {
			t.Errorf("%q is listed but does not resolve to itself", n)
		}
	}
	if names[len(names)-1] != "Deutschland" {
		t.Errorf("last region = %q, want Deutschland", names[len(names)-1])
	}
}

// TestNilCalendar checks that a nil calendar has no holidays.
func TestNilCalendar(t *testing.T) {
	var c *Calendar
	if c.Name(date(2026, 1, 1, berlin)) != "" || c.Is(date(2026, 1, 1, berlin)) {
		t.Error("nil calendar has holidays")
	}
	if got := c.Region(); got != "" {
		t.Errorf("nil Region() = %q, want empty", got)
	}
}

// TestInRange lists the holidays of a range of local days in order; a
// range over New Year spans two years.
func TestInRange(t *testing.T) {
	c := New("Baden-Württemberg")
	got := c.InRange(date(2025, 12, 20, berlin), date(2026, 1, 10, berlin))
	var names []string
	for _, h := range got {
		names = append(names, h.Day.Format("2006-01-02")+" "+h.Name)
	}
	want := "2025-12-25 Weihnachten, 2025-12-26 Zweiter Weihnachtsfeiertag, 2026-01-01 Neujahr, 2026-01-06 Epiphanias"
	if s := strings.Join(names, ", "); s != want {
		t.Errorf("InRange = %s\nwant %s", s, want)
	}
	for _, h := range got {
		if h.Day.Location() != berlin || h.Day.Hour() != 0 {
			t.Errorf("day %s is not a local midnight", h.Day)
		}
	}
	if n := len(c.InRange(date(2026, 1, 10, berlin), date(2026, 1, 10, berlin))); n != 0 {
		t.Errorf("empty range has %d holidays", n)
	}
}
