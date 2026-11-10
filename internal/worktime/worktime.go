// Package worktime estimates the active working time per day, category and
// project from the user's own messages, as a basis for timesheets
// (`stats --worktime`).
//
// Method: the human events of all sessions are put in one timeline. The
// gap between two consecutive events counts for the project of the later
// one: in full up to Settings.MaxGap, otherwise (a break, a meeting, the
// first message of a day) Settings.MinCredit. Projects are mapped to
// categories by ordered glob rules; a project without a rule is its own
// category. The sum per day and category is rounded to a grid. Meetings
// and work without an agent leave no events: the values are a lower bound.
//
// The functions are pure; the store supplies the events (store.HumanEvents).
package worktime

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// Rounding is the direction of Round.
type Rounding int

// Rounding modes.
const (
	Up      Rounding = iota // to the next grid line
	Down                    // to the previous grid line
	Nearest                 // to the closest grid line, halves up
)

// Roundings are the names accepted by ParseRounding, in order.
var Roundings = []string{"up", "down", "nearest"}

// String returns up, down or nearest.
func (r Rounding) String() string {
	if int(r) < len(Roundings) {
		return Roundings[r]
	}
	return "up"
}

// ParseRounding returns the Rounding named by s.
func ParseRounding(s string) (Rounding, error) {
	for i, n := range Roundings {
		if n == s {
			return Rounding(i), nil
		}
	}
	return 0, fmt.Errorf("invalid rounding %q (%s)", s, strings.Join(Roundings, ", "))
}

// Scope says what is rounded.
type Scope int

// Rounding scopes.
const (
	// ScopeCategory rounds the sum of a category per day once.
	ScopeCategory Scope = iota
	// ScopeProject rounds every project per day; the category is the sum
	// of its rounded projects.
	ScopeProject
)

// Scopes are the names accepted by ParseScope, in order.
var Scopes = []string{"category", "project"}

// String returns category or project.
func (s Scope) String() string {
	if int(s) < len(Scopes) {
		return Scopes[s]
	}
	return "category"
}

// ParseScope returns the Scope named by s.
func ParseScope(s string) (Scope, error) {
	for i, n := range Scopes {
		if n == s {
			return Scope(i), nil
		}
	}
	return 0, fmt.Errorf("invalid rounding scope %q (%s)", s, strings.Join(Scopes, ", "))
}

// Rule maps projects to a category: the first rule with a matching pattern
// wins. See NewRule for the pattern syntax.
type Rule struct {
	Name     string
	Patterns []string
	compiled project.Patterns
}

// NewRule compiles the patterns of a category: globs as project.CompilePatterns
// describes them (`*` matches any text, also across `/`, `?` one
// character, everything else is literal). They are matched whole against
// the project name, the git root (or working directory) as a path and with
// the home directory as `~`, and the remote URL. The name and at least one
// non-empty pattern are required.
func NewRule(name string, patterns []string) (Rule, error) {
	if strings.TrimSpace(name) == "" {
		return Rule{}, fmt.Errorf("category without a name")
	}
	p, err := project.CompilePatterns(patterns)
	if err != nil {
		return Rule{}, fmt.Errorf("category %q: %w", name, err)
	}
	return Rule{Name: name, Patterns: patterns, compiled: p}, nil
}

// Matches reports whether any pattern matches any of the targets.
func (r Rule) Matches(targets ...string) bool { return r.compiled.Matches(targets...) }

// ValidateRules checks that the category names are distinct.
func ValidateRules(rules []Rule) error {
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.Name] {
			return fmt.Errorf("category %q is listed twice", r.Name)
		}
		seen[r.Name] = true
	}
	return nil
}

// Settings are the parameters of the estimate; see the package comment.
type Settings struct {
	// MaxGap is the longest gap between two own messages that counts in
	// full; MinCredit is what a longer gap counts.
	MaxGap, MinCredit time.Duration
	// Grid is the rounding grid (15m); zero means no rounding.
	Grid time.Duration
	// Rounding is the direction, Scope what is rounded.
	Rounding Rounding
	Scope    Scope
	// Rules map projects to categories, in order; a project no rule
	// matches is its own category, named like the project.
	Rules []Rule
	// SummaryMaxChars is the length of the booking text of a category
	// and day in runes, 0 for no limit.
	SummaryMaxChars int
}

// credit is the time a gap counts; a negative gap marks the first event
// of a day.
func (s Settings) credit(gap time.Duration) time.Duration {
	if gap < 0 || gap > s.MaxGap {
		return s.MinCredit
	}
	return gap
}

// Round rounds d to the grid in the given direction; a zero grid returns d.
func Round(d, grid time.Duration, mode Rounding) time.Duration {
	if grid <= 0 {
		return d
	}
	q, r := d/grid, d%grid
	switch {
	case r == 0:
	case mode == Up, mode == Nearest && r*2 >= grid:
		q++
	}
	return q * grid
}

// Event is one own message with its session and project; the input of
// Compute. Project is the key of the activity dimension project (the
// session's git root, else its working directory), Dir is true for the
// latter, Remote the remote URL or "".
type Event struct {
	Time    time.Time
	Session string // session id
	Title   string // display name of the session
	Project string
	Dir     bool
	Remote  string
	// Text is the message, the source of the references (FindRefs) and
	// of the booking text.
	Text string
}

// Options are the day rule and the range of Compute.
type Options struct {
	// Location is the time zone of the days; default UTC.
	Location *time.Location
	// DayStart is the wall-clock offset at which a day begins (04:00 = 4h).
	DayStart time.Duration
	// Home abbreviates working directories without a repository to "~".
	Home string
	// Projects assigns the events to the configured projects ([[project]])
	// before anything else: an event whose key a definition matches counts
	// under the project's name, so several repositories can be one project
	// and the category rules see the configured name. The zero value
	// leaves every key as it is.
	Projects project.Resolver
	// From and To are the first and last day (local midnights) of the
	// report; zero means the first or last day with events.
	From, To time.Time
	// Holidays names public holidays; nil means there are none.
	Holidays store.Holidays
}

// Report is the result of Compute.
type Report struct {
	// Days are all days of the range in order, inactive ones without
	// categories.
	Days []Day
	// Categories sums every category over the range, sorted by name.
	Categories []Total
	// Total sums everything.
	Total Total
	// Uncategorized lists the projects with time in the range that no rule
	// maps to a category (each is its own category, so they are part of
	// Categories and Total): largest first, then by name.
	Uncategorized []Uncategorized
}

// Uncategorized is a project no category rule matches, with the credited
// time of the whole report range.
type Uncategorized struct {
	Name, Key, Remote string
	Raw               time.Duration
}

// Day is one day of a Report.
type Day struct {
	Day     time.Time // local midnight
	Label   string    // 2006-01-02
	Holiday string    // name of the public holiday, "" on an ordinary day
	// Raw and Rounded sum the categories of the day.
	Raw, Rounded time.Duration
	// Categories with time on the day, sorted by name.
	Categories []Category
}

// Active reports whether the day has any credited time.
func (d Day) Active() bool { return len(d.Categories) > 0 }

// Category is one category on one day.
type Category struct {
	Name string
	// Raw is the credited time, Rounded the time to book: the rounded Raw
	// (ScopeCategory) or the sum of the rounded projects (ScopeProject).
	Raw, Rounded time.Duration
	// Projects are the projects of the category with time on the day,
	// sorted by name; Sessions the sessions in order of their first message.
	Projects []Project
	Sessions []Session
	// Refs are the references of all sessions, deduplicated and sorted;
	// Text is the booking text (see Summary).
	Refs []Ref
	Text string
}

// Project is one project within a category on one day. Rounded is the
// rounded Raw whatever the scope; with ScopeCategory the projects' rounded
// values can sum to more than the category's. Refs are the references of
// the project's sessions, deduplicated and sorted.
type Project struct {
	Name, Key, Remote string
	Raw, Rounded      time.Duration
	Refs              []Ref
}

// Session is one session within a category on one day. Project is the
// key of its project, Prompt the first own message of the day with text,
// Refs the references of the title and the messages, sorted.
type Session struct {
	ID, Title string
	Project   string
	Prompt    string
	Raw       time.Duration
	Refs      []Ref
}

// Total sums a category (Name) or everything over the range: Raw, Rounded
// as the sum of the rounded day cells, Days with time.
type Total struct {
	Name         string
	Raw, Rounded time.Duration
	Days         int
}

// Compute credits the gaps between the events (any order) and aggregates
// them per day, category, project and session under the settings. Days
// run from opts.From to opts.To, or over the days with events.
func Compute(events []Event, s Settings, opts Options) Report {
	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}
	sorted := make([]Event, len(events))
	copy(sorted, events)
	for i := range sorted {
		// Whole seconds: the gaps, so every credit and every sum of them is
		// a whole number of seconds and the parts add up to the whole, however
		// the output cuts fractions (timestamps of the archive have
		// milliseconds). The error is below a second per day.
		sorted[i].Time = sorted[i].Time.Truncate(time.Second)
		if key := opts.Projects.Key(sorted[i].Project, sorted[i].Dir, sorted[i].Remote); key != sorted[i].Project {
			sorted[i].Project, sorted[i].Dir = key, false
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	type sessAcc struct {
		id, title, project, prompt string
		first                      time.Time
		raw                        time.Duration
		refs                       []Ref
	}
	type projAcc struct {
		key, remote string
		raw         time.Duration
	}
	type catAcc struct {
		raw      time.Duration
		projects map[string]*projAcc
		sessions map[string]*sessAcc
	}
	names := projectNames(sorted, opts.Home, opts.Projects)
	category, uncategorized := categories(names, sorted, s.Rules, opts.Home)
	days := map[time.Time]map[string]*catAcc{}
	var prev time.Time
	var prevDay time.Time
	var firstDay, lastDay time.Time
	for _, e := range sorted {
		day := store.DayOf(e.Time, loc, opts.DayStart)
		gap := -time.Nanosecond
		if day.Equal(prevDay) {
			gap = e.Time.Sub(prev)
		}
		prev, prevDay = e.Time, day
		credit := s.credit(gap)
		cats := days[day]
		if cats == nil {
			cats = map[string]*catAcc{}
			days[day] = cats
		}
		cat := category[e.Project]
		c := cats[cat]
		if c == nil {
			c = &catAcc{projects: map[string]*projAcc{}, sessions: map[string]*sessAcc{}}
			cats[cat] = c
		}
		c.raw += credit
		p := c.projects[e.Project]
		if p == nil {
			p = &projAcc{key: e.Project, remote: e.Remote}
			c.projects[e.Project] = p
		}
		p.raw += credit
		sa := c.sessions[e.Session]
		if sa == nil {
			sa = &sessAcc{id: e.Session, title: e.Title, project: e.Project, first: e.Time, refs: FindRefs(e.Title)}
			c.sessions[e.Session] = sa
		}
		sa.raw += credit
		if sa.prompt == "" {
			sa.prompt = e.Text
		}
		if refs := FindRefs(e.Text); len(refs) > 0 {
			sa.refs = MergeRefs(sa.refs, refs)
		}
		if firstDay.IsZero() || day.Before(firstDay) {
			firstDay = day
		}
		if day.After(lastDay) {
			lastDay = day
		}
	}

	from, to := firstDay, lastDay
	if !opts.From.IsZero() {
		from = opts.From
	}
	if !opts.To.IsZero() {
		to = opts.To
	}
	var out Report
	if from.IsZero() || to.IsZero() {
		return out
	}
	totals := map[string]*Total{}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		d := Day{Day: day, Label: day.Format("2006-01-02")}
		if opts.Holidays != nil {
			d.Holiday = opts.Holidays.Name(day)
		}
		for name, c := range days[day] {
			cat := Category{Name: name, Raw: c.raw}
			for _, p := range c.projects {
				cat.Projects = append(cat.Projects, Project{Name: names[p.key], Key: p.key, Remote: p.remote, Raw: p.raw, Rounded: Round(p.raw, s.Grid, s.Rounding)})
			}
			sort.Slice(cat.Projects, func(i, j int) bool { return cat.Projects[i].Name < cat.Projects[j].Name })
			for _, sa := range c.sessions {
				cat.Sessions = append(cat.Sessions, Session{ID: sa.id, Title: sa.title, Project: sa.project, Prompt: sa.prompt, Raw: sa.raw, Refs: sa.refs})
			}
			sort.SliceStable(cat.Sessions, func(i, j int) bool {
				a, b := c.sessions[cat.Sessions[i].ID], c.sessions[cat.Sessions[j].ID]
				if !a.first.Equal(b.first) {
					return a.first.Before(b.first)
				}
				return a.id < b.id
			})
			for i := range cat.Projects {
				var lists [][]Ref
				for _, sess := range cat.Sessions {
					if sess.Project == cat.Projects[i].Key {
						lists = append(lists, sess.Refs)
					}
				}
				cat.Projects[i].Refs = MergeRefs(lists...)
			}
			var lists [][]Ref
			for _, p := range cat.Projects {
				lists = append(lists, p.Refs)
			}
			cat.Refs = MergeRefs(lists...)
			cat.Text = Summary(cat, s.SummaryMaxChars)
			switch s.Scope {
			case ScopeProject:
				for _, p := range cat.Projects {
					cat.Rounded += p.Rounded
				}
			default:
				cat.Rounded = Round(cat.Raw, s.Grid, s.Rounding)
			}
			d.Categories = append(d.Categories, cat)
			d.Raw += cat.Raw
			d.Rounded += cat.Rounded
			tc := totals[name]
			if tc == nil {
				tc = &Total{Name: name}
				totals[name] = tc
			}
			tc.Raw += cat.Raw
			tc.Rounded += cat.Rounded
			tc.Days++
		}
		sort.Slice(d.Categories, func(i, j int) bool { return d.Categories[i].Name < d.Categories[j].Name })
		if d.Active() {
			out.Total.Days++
		}
		out.Total.Raw += d.Raw
		out.Total.Rounded += d.Rounded
		out.Days = append(out.Days, d)
	}
	for _, tc := range totals {
		out.Categories = append(out.Categories, *tc)
	}
	sort.Slice(out.Categories, func(i, j int) bool { return out.Categories[i].Name < out.Categories[j].Name })
	// Uncategorized sums the projects of the output days, so it is always a
	// part of Total.
	unc := map[string]*Uncategorized{}
	for _, d := range out.Days {
		for _, c := range d.Categories {
			for _, p := range c.Projects {
				if !uncategorized[p.Key] {
					continue
				}
				u := unc[p.Key]
				if u == nil {
					u = &Uncategorized{Name: p.Name, Key: p.Key, Remote: p.Remote}
					unc[p.Key] = u
				}
				u.Raw += p.Raw
			}
		}
	}
	for _, u := range unc {
		out.Uncategorized = append(out.Uncategorized, *u)
	}
	sort.Slice(out.Uncategorized, func(i, j int) bool {
		a, b := out.Uncategorized[i], out.Uncategorized[j]
		if a.Raw != b.Raw {
			return a.Raw > b.Raw
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Key < b.Key
	})
	return out
}

// projectNames maps every project key of the events to its display name:
// configured names (as Options.Projects assigned them) stay, the rest get
// the derived names (project.Names).
func projectNames(events []Event, home string, r project.Resolver) map[string]string {
	var keys, dirs []string
	seen := map[string]bool{}
	for _, e := range events {
		if seen[e.Project] {
			continue
		}
		seen[e.Project] = true
		keys = append(keys, e.Project)
		if e.Dir {
			dirs = append(dirs, e.Project)
		}
	}
	return r.Names(keys, dirs, home)
}

// categories maps every project key to its category: the name of the
// first matching rule, else the project name. The second result is the set
// of project keys no rule matched.
func categories(names map[string]string, events []Event, rules []Rule, home string) (map[string]string, map[string]bool) {
	out := map[string]string{}
	unmatched := map[string]bool{}
	for _, e := range events {
		if _, done := out[e.Project]; done {
			continue
		}
		cat, ok := match(names[e.Project], e.Project, e.Remote, home, rules)
		out[e.Project] = cat
		unmatched[e.Project] = !ok
	}
	return out, unmatched
}

// Categorize returns the category of a project: the name of the first
// rule that matches its name, its key (as a path and with home as "~"),
// or its remote URL; without a match the project name.
func Categorize(name, key, remote, home string, rules []Rule) string {
	cat, _ := match(name, key, remote, home, rules)
	return cat
}

// match is Categorize that also reports whether a rule matched.
func match(name, key, remote, home string, rules []Rule) (string, bool) {
	targets := []string{name, key, project.Tilde(key, home)}
	if remote != "" {
		targets = append(targets, remote)
	}
	for _, r := range rules {
		if r.Matches(targets...) {
			return r.Name, true
		}
	}
	return name, false
}
