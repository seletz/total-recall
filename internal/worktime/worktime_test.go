package worktime

import (
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/project"
)

// cet is the fixed zone of the test events (no DST in the test dates).
var cet = time.FixedZone("CET", 3600)

// at returns a local time of 2026-01-05 (a Monday) plus days.
func at(day, hh, mm int) time.Time {
	return time.Date(2026, 1, 5+day, hh, mm, 0, 0, cet)
}

func defaults() Settings {
	return Settings{MaxGap: 20 * time.Minute, MinCredit: 3 * time.Minute, Grid: 15 * time.Minute, Rounding: Up, Scope: ScopeCategory}
}

// ev is a human event of a session in a project.
func ev(t time.Time, session, project string) Event {
	return Event{Time: t, Session: session, Title: "title " + session, Project: project, Remote: "https://example.test/o/" + project[strings.LastIndex(project, "/")+1:]}
}

func TestCredit(t *testing.T) {
	s := defaults()
	tests := []struct {
		name string
		gap  time.Duration
		want time.Duration
	}{
		{"below cap", 7 * time.Minute, 7 * time.Minute},
		{"at cap", 20 * time.Minute, 20 * time.Minute},
		{"just above cap", 20*time.Minute + time.Second, 3 * time.Minute},
		{"far above cap", 3 * time.Hour, 3 * time.Minute},
		{"zero gap", 0, 0},
		{"first of day", -1, 3 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.credit(tt.gap); got != tt.want {
				t.Errorf("credit(%s) = %s, want %s", tt.gap, got, tt.want)
			}
		})
	}
}

func TestRound(t *testing.T) {
	const grid = 15 * time.Minute
	tests := []struct {
		raw  time.Duration
		mode Rounding
		want time.Duration
	}{
		{0, Up, 0},
		{0, Nearest, 0},
		{time.Minute, Up, 15 * time.Minute},
		{time.Minute, Down, 0},
		{time.Minute, Nearest, 0},
		{15 * time.Minute, Up, 15 * time.Minute},
		{15 * time.Minute, Down, 15 * time.Minute},
		{15 * time.Minute, Nearest, 15 * time.Minute},
		{22 * time.Minute, Up, 30 * time.Minute},
		{22 * time.Minute, Down, 15 * time.Minute},
		{22 * time.Minute, Nearest, 15 * time.Minute},
		{22*time.Minute + 30*time.Second, Nearest, 30 * time.Minute}, // half rounds up
		{23 * time.Minute, Nearest, 30 * time.Minute},
		{3*time.Hour + 1*time.Second, Up, 3*time.Hour + 15*time.Minute},
	}
	for _, tt := range tests {
		if got := Round(tt.raw, grid, tt.mode); got != tt.want {
			t.Errorf("Round(%s, %s, %s) = %s, want %s", tt.raw, grid, tt.mode, got, tt.want)
		}
	}
	if got := Round(22*time.Minute, 0, Up); got != 22*time.Minute {
		t.Errorf("Round without a grid = %s, want the raw value", got)
	}
}

func TestParse(t *testing.T) {
	for _, s := range []string{"up", "down", "nearest"} {
		r, err := ParseRounding(s)
		if err != nil || r.String() != s {
			t.Errorf("ParseRounding(%q) = %v, %v", s, r, err)
		}
	}
	if _, err := ParseRounding("ceil"); err == nil {
		t.Error("ParseRounding(ceil): no error")
	}
	for _, s := range []string{"category", "project"} {
		sc, err := ParseScope(s)
		if err != nil || sc.String() != s {
			t.Errorf("ParseScope(%q) = %v, %v", s, sc, err)
		}
	}
	if _, err := ParseScope("cell"); err == nil {
		t.Error("ParseScope(cell): no error")
	}
}

func TestRule(t *testing.T) {
	tests := []struct {
		pattern string
		target  string
		want    bool
	}{
		{"kompass", "kompass", true},
		{"kompass", "kompass-gpx", false},
		{"kompass*", "kompass-gpx", true},
		{"https://git.invalid/mara/*", "https://git.invalid/mara/kompass", true},
		{"*git.invalid/mara/*", "https://git.invalid/mara/kompass", true}, // * crosses "/"
		{"*/mara/*", "https://git.invalid/other/kompass", false},
		{"~/src/*", "~/src/kompass", true},
		{"~/src/*", "/home/mara/src/kompass", false},
		{"k?mpass", "kompass", true},
		{"k?mpass", "koompass", false},
		{"*", "", true},
		{"a.b", "axb", false}, // a dot is literal
		{"[x]", "[x]", true},  // brackets are literal
	}
	for _, tt := range tests {
		r, err := NewRule("c", []string{tt.pattern})
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Matches(tt.target); got != tt.want {
			t.Errorf("%q matches %q = %v, want %v", tt.pattern, tt.target, got, tt.want)
		}
	}
	if _, err := NewRule("", []string{"*"}); err == nil {
		t.Error("empty name: no error")
	}
	if _, err := NewRule("c", nil); err == nil {
		t.Error("no patterns: no error")
	}
	if _, err := NewRule("c", []string{""}); err == nil {
		t.Error("empty pattern: no error")
	}
}

func TestValidateRules(t *testing.T) {
	a, _ := NewRule("a", []string{"*"})
	b, _ := NewRule("b", []string{"x"})
	a2, _ := NewRule("a", []string{"y"})
	if err := ValidateRules([]Rule{a, b}); err != nil {
		t.Errorf("distinct names: %v", err)
	}
	if err := ValidateRules([]Rule{a, b, a2}); err == nil || !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("duplicate name: %v", err)
	}
}

// rules builds rules or fails the test.
func rules(t *testing.T, pairs ...string) []Rule {
	t.Helper()
	var out []Rule
	for i := 0; i+1 < len(pairs); i += 2 {
		r, err := NewRule(pairs[i], strings.Split(pairs[i+1], ","))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// cell finds a category of a day in a report.
func cell(r Report, day, category string) (Category, bool) {
	for _, d := range r.Days {
		if d.Label != day {
			continue
		}
		for _, c := range d.Categories {
			if c.Name == category {
				return c, true
			}
		}
	}
	return Category{}, false
}

const (
	alpha = "/w/alpha"
	beta  = "/w/beta"
)

func TestComputeGapsAndCategories(t *testing.T) {
	// Monday: alpha session a1 at 09:00, 09:10, 09:40 (30m gap: 3m credit);
	// beta session b1 at 09:45 (5m after alpha: cross-session gap counts for
	// beta), then alpha again at 09:50 (5m, credited to alpha).
	events := []Event{
		ev(at(0, 9, 0), "a1", alpha),
		ev(at(0, 9, 10), "a1", alpha),
		ev(at(0, 9, 40), "a1", alpha),
		ev(at(0, 9, 45), "b1", beta),
		ev(at(0, 9, 50), "a1", alpha),
	}
	opts := Options{Location: cet, DayStart: 4 * time.Hour}
	r := Compute(events, defaults(), opts)
	if len(r.Days) != 1 || r.Days[0].Label != "2026-01-05" {
		t.Fatalf("days = %+v", r.Days)
	}
	a, ok := cell(r, "2026-01-05", "alpha")
	if !ok || a.Raw != 3*time.Minute+10*time.Minute+3*time.Minute+5*time.Minute || a.Rounded != 30*time.Minute {
		t.Errorf("alpha = %+v (ok %v)", a, ok)
	}
	b, ok := cell(r, "2026-01-05", "beta")
	if !ok || b.Raw != 5*time.Minute || b.Rounded != 15*time.Minute {
		t.Errorf("beta = %+v (ok %v)", b, ok)
	}
	if len(a.Sessions) != 1 || a.Sessions[0].ID != "a1" || a.Sessions[0].Title != "title a1" || a.Sessions[0].Raw != a.Raw {
		t.Errorf("alpha sessions = %+v", a.Sessions)
	}
	if len(a.Projects) != 1 || a.Projects[0].Name != "alpha" || a.Projects[0].Key != alpha || a.Projects[0].Remote != "https://example.test/o/alpha" {
		t.Errorf("alpha projects = %+v", a.Projects)
	}
	if d := r.Days[0]; d.Raw != 26*time.Minute || d.Rounded != 45*time.Minute {
		t.Errorf("day = raw %s rounded %s", d.Raw, d.Rounded)
	}
	if r.Total.Raw != 26*time.Minute || r.Total.Rounded != 45*time.Minute || r.Total.Days != 1 {
		t.Errorf("total = %+v", r.Total)
	}
	if len(r.Categories) != 2 || r.Categories[0].Name != "alpha" || r.Categories[1].Name != "beta" || r.Categories[1].Days != 1 {
		t.Errorf("categories = %+v", r.Categories)
	}

	// With a rule both projects fall into one category; the sum is rounded
	// once (scope category) or per project (scope project).
	s := defaults()
	s.Rules = rules(t, "work", "https://example.test/o/*")
	r = Compute(events, s, opts)
	w, ok := cell(r, "2026-01-05", "work")
	if !ok || len(r.Days[0].Categories) != 1 || w.Raw != 26*time.Minute || w.Rounded != 30*time.Minute {
		t.Errorf("scope category: work = %+v (ok %v)", w, ok)
	}
	if len(w.Projects) != 2 || w.Projects[0].Name != "alpha" || w.Projects[0].Rounded != 30*time.Minute || w.Projects[1].Name != "beta" || w.Projects[1].Rounded != 15*time.Minute {
		t.Errorf("scope category: projects = %+v", w.Projects)
	}
	if len(w.Sessions) != 2 || w.Sessions[0].ID != "a1" || w.Sessions[1].ID != "b1" {
		t.Errorf("scope category: sessions = %+v", w.Sessions)
	}
	s.Scope = ScopeProject
	r = Compute(events, s, opts)
	if w, _ = cell(r, "2026-01-05", "work"); w.Raw != 26*time.Minute || w.Rounded != 45*time.Minute {
		t.Errorf("scope project: work = %+v", w)
	}
}

func TestComputeRuleOrderAndTargets(t *testing.T) {
	events := []Event{
		{Time: at(0, 9, 0), Session: "a1", Project: "/home/u/src/alpha", Remote: "https://example.test/o/alpha"},
		{Time: at(0, 9, 5), Session: "b1", Project: "/home/u/src/beta", Remote: "https://example.test/o/beta"},
		{Time: at(0, 9, 10), Session: "g1", Project: "/home/u/notes", Dir: true},
	}
	opts := Options{Location: cet, DayStart: 4 * time.Hour, Home: "/home/u"}
	tests := []struct {
		name  string
		rules []string
		want  map[string]string // project name -> category
	}{
		{"none", nil, map[string]string{"alpha": "alpha", "beta": "beta", "~/notes": "~/notes"}},
		{"first match wins", []string{"first", "*alpha*", "second", "*"}, map[string]string{"alpha": "first", "beta": "second", "~/notes": "second"}},
		{"order reversed", []string{"second", "*", "first", "*alpha*"}, map[string]string{"alpha": "second", "beta": "second", "~/notes": "second"}},
		{"by name", []string{"n", "beta"}, map[string]string{"alpha": "alpha", "beta": "n", "~/notes": "~/notes"}},
		{"by git root", []string{"p", "/home/u/src/*"}, map[string]string{"alpha": "p", "beta": "p", "~/notes": "~/notes"}},
		{"by tilde path", []string{"p", "~/src/*", "d", "~/notes"}, map[string]string{"alpha": "p", "beta": "p", "~/notes": "d"}},
		{"by remote", []string{"r", "https://example.test/o/beta"}, map[string]string{"alpha": "alpha", "beta": "r", "~/notes": "~/notes"}},
		{"several patterns", []string{"r", "nomatch,*beta*,*notes*"}, map[string]string{"alpha": "alpha", "beta": "r", "~/notes": "r"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := defaults()
			s.Rules = rules(t, tt.rules...)
			r := Compute(events, s, opts)
			got := map[string]string{}
			for _, c := range r.Days[0].Categories {
				for _, p := range c.Projects {
					got[p.Name] = c.Name
				}
			}
			for name, want := range tt.want {
				if got[name] != want {
					t.Errorf("%s -> %q, want %q (all: %v)", name, got[name], want, got)
				}
			}
		})
	}
}

func TestComputeDayBoundary(t *testing.T) {
	// 23:50 and 00:05 (+1) are one day with day_start 04:00 (gap 15m in
	// full) and two days at midnight (00:05 is the first of its day: 3m).
	events := []Event{ev(at(0, 23, 50), "a1", alpha), ev(at(1, 0, 5), "a1", alpha)}
	r := Compute(events, defaults(), Options{Location: cet, DayStart: 4 * time.Hour})
	if len(r.Days) != 1 || r.Days[0].Label != "2026-01-05" || r.Days[0].Raw != 18*time.Minute {
		t.Errorf("04:00: days = %+v", r.Days)
	}
	r = Compute(events, defaults(), Options{Location: cet})
	if len(r.Days) != 2 || r.Days[0].Raw != 3*time.Minute || r.Days[1].Raw != 3*time.Minute || r.Days[1].Label != "2026-01-06" {
		t.Errorf("midnight: days = %+v", r.Days)
	}
}

func TestComputeRangeAndHolidays(t *testing.T) {
	events := []Event{ev(at(1, 9, 0), "a1", alpha)} // Tuesday only
	hol := holidayFunc(func(d time.Time) string {
		if d.Day() == 6 {
			return "Epiphanias"
		}
		return ""
	})
	r := Compute(events, defaults(), Options{Location: cet, DayStart: 4 * time.Hour, From: at(0, 0, 0), To: at(2, 0, 0), Holidays: hol})
	if len(r.Days) != 3 {
		t.Fatalf("days = %d, want 3", len(r.Days))
	}
	if d := r.Days[0]; d.Label != "2026-01-05" || d.Raw != 0 || len(d.Categories) != 0 || d.Holiday != "" {
		t.Errorf("Mon = %+v", d)
	}
	if d := r.Days[1]; d.Holiday != "Epiphanias" || d.Raw != 3*time.Minute || d.Rounded != 15*time.Minute {
		t.Errorf("Tue = %+v", d)
	}
	if r.Total.Days != 1 || len(r.Categories) != 1 || r.Categories[0].Days != 1 {
		t.Errorf("totals = %+v %+v", r.Total, r.Categories)
	}
	// Without events and without a range there are no days.
	if r := Compute(nil, defaults(), Options{Location: cet}); len(r.Days) != 0 || len(r.Categories) != 0 {
		t.Errorf("empty = %+v", r)
	}
	// Without events but with a range every day is listed, inactive.
	if r := Compute(nil, defaults(), Options{Location: cet, From: at(0, 0, 0), To: at(1, 0, 0)}); len(r.Days) != 2 || r.Total.Days != 0 {
		t.Errorf("empty range = %+v", r)
	}
}

// TestComputeUnsorted checks that the order of the input does not matter.
func TestComputeUnsorted(t *testing.T) {
	events := []Event{ev(at(0, 9, 10), "a1", alpha), ev(at(0, 9, 0), "a1", alpha)}
	r := Compute(events, defaults(), Options{Location: cet})
	if r.Total.Raw != 13*time.Minute {
		t.Errorf("raw = %s, want 13m", r.Total.Raw)
	}
}

// TestComputeCollidingNames checks that two repositories with the same base
// name get distinct project names (internal/project) and categories.
func TestComputeCollidingNames(t *testing.T) {
	events := []Event{
		{Time: at(0, 9, 0), Session: "a", Project: "/w/x/app"},
		{Time: at(0, 9, 5), Session: "b", Project: "/w/y/app"},
	}
	r := Compute(events, defaults(), Options{Location: cet})
	if n := len(r.Days[0].Categories); n != 2 {
		t.Fatalf("categories = %d, want 2: %+v", n, r.Days[0].Categories)
	}
	if a, b := r.Days[0].Categories[0].Name, r.Days[0].Categories[1].Name; a != "x/app" || b != "y/app" {
		t.Errorf("names = %q, %q", a, b)
	}
}

type holidayFunc func(time.Time) string

func (f holidayFunc) Name(d time.Time) string { return f(d) }

func TestComputeProjectDefinitions(t *testing.T) {
	defs := []project.Definition{
		mustDefinition(t, "kompass", "https://example.test/o/kompass-*"),
		mustDefinition(t, "notizbox", "~/notes"),
	}
	res, err := project.NewResolver(defs, "/home/mara")
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{
		ev(at(0, 9, 0), "a1", "/w/kompass-api"),
		ev(at(0, 9, 10), "b1", "/w/kompass-web"),
		ev(at(0, 9, 20), "c1", "/w/iris"),
		{Time: at(0, 9, 30), Session: "d1", Project: "/home/mara/notes", Dir: true},
	}
	opts := Options{Location: cet, DayStart: 4 * time.Hour, Home: "/home/mara", Projects: res}
	r := Compute(events, defaults(), opts)
	if n := len(r.Days[0].Categories); n != 3 {
		t.Fatalf("categories = %d, want 3: %+v", n, r.Days[0].Categories)
	}
	k, ok := cell(r, "2026-01-05", "kompass")
	if !ok || k.Raw != 3*time.Minute+10*time.Minute {
		t.Fatalf("kompass = %+v (ok %v)", k, ok)
	}
	// Two repositories, one project: one row, keyed by the project name.
	if len(k.Projects) != 1 || k.Projects[0].Name != "kompass" || k.Projects[0].Key != "kompass" {
		t.Errorf("kompass projects = %+v", k.Projects)
	}
	if len(k.Sessions) != 2 || k.Sessions[0].Project != "kompass" || k.Sessions[1].Project != "kompass" {
		t.Errorf("kompass sessions = %+v", k.Sessions)
	}
	if n, ok := cell(r, "2026-01-05", "notizbox"); !ok || len(n.Projects) != 1 || n.Projects[0].Name != "notizbox" {
		t.Errorf("notizbox = %+v (ok %v)", n, ok)
	}
	if i, ok := cell(r, "2026-01-05", "iris"); !ok || i.Projects[0].Key != "/w/iris" {
		t.Errorf("iris = %+v (ok %v)", i, ok)
	}

	// Category rules see the configured project name.
	s := defaults()
	s.Rules = rules(t, "kunde", "kompass,notizbox")
	r = Compute(events, s, opts)
	if n := len(r.Days[0].Categories); n != 2 {
		t.Fatalf("categories with rule = %d, want 2: %+v", n, r.Days[0].Categories)
	}
	if c, ok := cell(r, "2026-01-05", "kunde"); !ok || len(c.Projects) != 2 || c.Projects[0].Name != "kompass" || c.Projects[1].Name != "notizbox" {
		t.Errorf("kunde = %+v (ok %v)", c, ok)
	}
}

func mustDefinition(t *testing.T, name string, match ...string) project.Definition {
	t.Helper()
	d, err := project.NewDefinition(name, "", "", match)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
