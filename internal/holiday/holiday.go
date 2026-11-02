// Package holiday knows the public holidays of Germany and its
// Bundesländer (github.com/wlbr/feiertage), independent of CLI and TUI:
// the store attributes days to them, the activity per day measures no
// target on them and every view names them.
//
// A Calendar is built for one Bundesland by name; an unknown or empty name
// yields the nationwide holidays only. Days are compared by their local
// calendar date, so callers pass instants in the time zone of the days.
package holiday

import (
	"strings"
	"sync"
	"time"

	"github.com/wlbr/feiertage"
)

// region is one selectable set of holidays.
type region struct {
	name string
	fn   func(int, ...bool) feiertage.Region
}

// regions lists the Bundesländer by their official names and the
// nationwide set last; see Regions.
var regions = []region{
	{"Baden-Württemberg", feiertage.BadenWürttemberg},
	{"Bayern", feiertage.Bayern},
	{"Berlin", feiertage.Berlin},
	{"Brandenburg", feiertage.Brandenburg},
	{"Bremen", feiertage.Bremen},
	{"Hamburg", feiertage.Hamburg},
	{"Hessen", feiertage.Hessen},
	{"Mecklenburg-Vorpommern", feiertage.MecklenburgVorpommern},
	{"Niedersachsen", feiertage.Niedersachsen},
	{"Nordrhein-Westfalen", feiertage.NordrheinWestfalen},
	{"Rheinland-Pfalz", feiertage.RheinlandPfalz},
	{"Saarland", feiertage.Saarland},
	{"Sachsen", feiertage.Sachsen},
	{"Sachsen-Anhalt", feiertage.SachsenAnhalt},
	{"Schleswig-Holstein", feiertage.SchleswigHolstein},
	{"Thüringen", feiertage.Thüringen},
	{Nationwide, feiertage.Deutschland},
}

// Nationwide is the name of the region with the holidays every
// Bundesland shares; it is the fallback for an unknown name.
const Nationwide = "Deutschland"

// Regions returns the accepted region names: the sixteen Bundesländer in
// alphabetical order and Nationwide last.
func Regions() []string {
	out := make([]string, len(regions))
	for i, r := range regions {
		out[i] = r.name
	}
	return out
}

// normalize makes a name comparable: lower case, without hyphens and
// spaces, so "Baden-Württemberg", "BadenWürttemberg" and
// "baden württemberg" are the same.
func normalize(name string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(strings.ToLower(name))
}

// lookup finds the region of a name; ok is false for an unknown name.
func lookup(name string) (region, bool) {
	n := normalize(name)
	for _, r := range regions {
		if normalize(r.name) == n {
			return r, true
		}
	}
	return region{}, false
}

// Known reports whether name is one of Regions, in any spelling normalize
// accepts.
func Known(name string) bool {
	_, ok := lookup(name)
	return ok
}

// Holiday is one public holiday: the local midnight of its day and its
// German name.
type Holiday struct {
	Day  time.Time
	Name string
}

// date is a calendar date, the key of the per-year index.
type ymd struct {
	y int
	m time.Month
	d int
}

// Calendar is the public holidays of one region. It caches the holidays
// per year and is safe for concurrent use; a nil Calendar has no holidays.
type Calendar struct {
	region region
	mu     sync.Mutex
	years  map[int]map[ymd]string
}

// New returns the calendar of the Bundesland named bundesland (see
// Regions; spelling with or without hyphens, case does not matter). An
// unknown or empty name yields the nationwide holidays.
func New(bundesland string) *Calendar {
	r, ok := lookup(bundesland)
	if !ok {
		r, _ = lookup(Nationwide)
	}
	return &Calendar{region: r, years: map[int]map[ymd]string{}}
}

// Region returns the name of the region in use: the Bundesland, or
// Nationwide after a fallback. Empty for a nil calendar.
func (c *Calendar) Region() string {
	if c == nil {
		return ""
	}
	return c.region.name
}

// year returns the holidays of one year by date, computing them once.
func (c *Calendar) year(y int) map[ymd]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.years[y]; ok {
		return m
	}
	m := map[ymd]string{}
	for _, f := range c.region.fn(y).Feiertage {
		yy, mm, dd := f.Date()
		m[ymd{yy, mm, dd}] = f.Text
	}
	c.years[y] = m
	return m
}

// Name returns the name of the holiday on the calendar date of day (in
// day's location), or "" on an ordinary day.
func (c *Calendar) Name(day time.Time) string {
	if c == nil {
		return ""
	}
	y, m, d := day.Date()
	return c.year(y)[ymd{y, m, d}]
}

// Is reports whether the calendar date of day is a holiday.
func (c *Calendar) Is(day time.Time) bool { return c.Name(day) != "" }

// InRange lists the holidays of the local days from first to last
// (inclusive, local midnights in the same location) in order.
func (c *Calendar) InRange(first, last time.Time) []Holiday {
	var out []Holiday
	if c == nil {
		return out
	}
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if n := c.Name(d); n != "" {
			out = append(out, Holiday{Day: d, Name: n})
		}
	}
	return out
}
