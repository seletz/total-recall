package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// worktimeCategories maps the Activity fixture's repositories to one
// category by remote URL and everything else to "sonstiges".
const worktimeCategories = `
[[worktime.category]]
name = "example"
match = ["https://example.test/o/*"]

[[worktime.category]]
name = "sonstiges"
match = "*"
`

// appendConfig appends content to the test config file.
func appendConfig(t *testing.T, e *testEnv, content string) {
	t.Helper()
	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append(cfg, []byte(content)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStatsWorktime compares the text and JSON output of --worktime with
// golden files (go test ./internal/cli -run TestStatsWorktime -update).
func TestStatsWorktime(t *testing.T) {
	tests := []struct {
		name   string
		config string
		args   []string
	}{
		{"ThisWeek", "", []string{"--range", "this-week"}},
		{"LastWeek", "", []string{"--range", "last-week"}},
		{"LastMonth", "", []string{"--range", "last-month"}},
		{"DefaultIsThisWeek", "", nil},
		{"SinceUntil", "", []string{"--since", "2025-12-15", "--until", "2025-12-21"}},
		{"Month", "", []string{"--month", "2025-12"}},
		{"Sessions", "", []string{"--range", "last-month", "--sessions"}},
		{"Categories", worktimeCategories, []string{"--range", "last-month"}},
		{"CategoriesSessions", worktimeCategories, []string{"--range", "last-month", "--sessions"}},
		{"Empty", "", []string{"--since", "2026-03-02", "--until", "2026-03-08"}},
		{"ThisWeekJSON", "", []string{"--range", "this-week", "--json"}},
		{"CategoriesJSON", worktimeCategories, []string{"--since", "2025-12-15", "--until", "2025-12-21", "--json"}},
		{"EmptyJSON", "", []string{"--since", "2026-03-02", "--until", "2026-03-08", "--json"}},
		{"EmptyJSONL", "", []string{"--since", "2026-03-02", "--until", "2026-03-08", "--format", "jsonl"}},
	}
	_, db := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.now = activityNow
			appendConfig(t, e, tt.config)
			r := runActivity(t, e, db, append([]string{"--worktime"}, tt.args...)...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			golden.RequireEqual(t, []byte(r.stdout))
		})
	}
}

// TestStatsWorktimeDemo renders one week of the synthetic demo archive
// with categories, as the docs show it.
func TestStatsWorktimeDemo(t *testing.T) {
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	appendConfig(t, e, `
[[worktime.category]]
name = "kompass"
match = ["https://git.invalid/mara/kompass"]

[[worktime.category]]
name = "privat"
match = ["~/dotfiles", "~/notizen"]
`)
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"Week", []string{"--since", "2026-10-26", "--until", "2026-10-30"}},
		{"WeekSessions", []string{"--since", "2026-10-26", "--until", "2026-10-27", "--sessions"}},
		{"WeekSummary", []string{"--since", "2026-10-26", "--until", "2026-10-27", "--summary"}},
		{"WeekSessionsSummary", []string{"--since", "2026-10-26", "--until", "2026-10-26", "--sessions", "--summary"}},
		{"WeekJSON", []string{"--since", "2026-10-26", "--until", "2026-10-26", "--json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := runActivity(t, e, db, append([]string{"--worktime"}, tt.args...)...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			golden.RequireEqual(t, []byte(r.stdout))
		})
	}
}

// cellOf finds a category of a day in the JSON output.
func cellOf(out WorktimeJSON, day, category string) (WorktimeCellJSON, bool) {
	for _, d := range out.Days {
		if d.Day != day {
			continue
		}
		for _, c := range d.Categories {
			if c.Category == category {
				return c, true
			}
		}
	}
	return WorktimeCellJSON{}, false
}

// TestStatsWorktimeValues pins hand-computed values of the Activity
// fixture: A4 on 2025-12-18 has prompts at 13:20, 13:35 and 13:50 (3m for
// the first of the day, then two gaps of 15m: 33m, rounded up 45m); A6 on
// 2026-01-08 has 17:30 and 17:55 (25m is above the cap: 3m + 3m).
func TestStatsWorktimeValues(t *testing.T) {
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	out := decode[WorktimeJSON](t, runActivity(t, e, db, "--worktime", "--month", "2025-12", "--json").stdout)
	if out.MaxGapSeconds != 20*60 || out.MinCreditSeconds != 3*60 || out.GridSeconds != 15*60 || out.Rounding != "up" || out.RoundingScope != "category" || out.DayStart != "04:00" {
		t.Errorf("settings = %+v", out)
	}
	c, ok := cellOf(out, "2025-12-18", "alpha")
	if !ok || c.RawSeconds != 33*60 || c.RoundedSeconds != 45*60 {
		t.Errorf("2025-12-18 alpha = %+v (ok %v)", c, ok)
	}
	if len(c.Sessions) != 1 || c.Sessions[0].RawSeconds != 33*60 || c.Sessions[0].Title != "" || !strings.HasPrefix(c.Sessions[0].ID, "claude:a1a1a1a1-0004") {
		t.Errorf("2025-12-18 alpha sessions = %+v", c.Sessions)
	}
	if len(c.Projects) != 1 || c.Projects[0].Project != "alpha" || c.Projects[0].Key != "/w/alpha" || c.Projects[0].Remote != "https://example.test/o/alpha" || c.Projects[0].RoundedSeconds != 45*60 {
		t.Errorf("2025-12-18 alpha projects = %+v", c.Projects)
	}
	if c, ok := cellOf(out, "2025-12-04", "beta"); !ok || c.RawSeconds != 3*60 || len(c.Sessions) != 1 || c.Sessions[0].Title != "Beta release" {
		t.Errorf("2025-12-04 beta = %+v (ok %v)", c, ok)
	}
	if c, ok := cellOf(out, "2025-12-06", "/w/gamma"); !ok || c.RawSeconds != 3*60 || len(c.Projects) != 1 || c.Projects[0].Remote != "" {
		t.Errorf("2025-12-06 gamma = %+v (ok %v)", c, ok)
	}
	// W1 on Mon 2025-12-08 00:30 belongs to Sunday 12-07 under the 04:00 rule.
	if c, ok := cellOf(out, "2025-12-07", "alpha"); !ok || c.RawSeconds != 3*60 {
		t.Errorf("2025-12-07 alpha (worktree, past midnight) = %+v (ok %v)", c, ok)
	}
	if _, ok := cellOf(out, "2025-12-08", "alpha"); ok {
		t.Error("2025-12-08 has alpha time, want none")
	}
	// December: alpha 12-01 (09:15 and 09:40, 25m above the cap: 3m + 3m),
	// 12-03 3m, 12-07 3m, 12-12 3m, 12-18 33m, 12-27 3m, 12-31 (23:40 and
	// 00:05 (+1), 25m: 3m + 3m) = 57m raw over 7 days, rounded 6 × 15m + 45m
	// = 2h15m. Beta: 12-04 3m, 12-09 (10:00 and 10:20, exactly the cap: 3m
	// + 20m = 23m, rounded 30m), 12-23 3m = 29m. Gamma: 12-06 and 12-16 3m
	// each. Total 92m raw over 12 days, 3h45m rounded.
	for _, tc := range out.Categories {
		if tc.Category == "alpha" && (tc.RawSeconds != 57*60 || tc.RoundedSeconds != 135*60 || tc.Days != 7) {
			t.Errorf("alpha total = %+v", tc)
		}
	}
	if c, ok := cellOf(out, "2025-12-09", "beta"); !ok || c.RawSeconds != 23*60 || c.RoundedSeconds != 30*60 {
		t.Errorf("2025-12-09 beta (gap at the cap) = %+v (ok %v)", c, ok)
	}
	if out.Total.Days != 12 || out.Total.RawSeconds != 92*60 || out.Total.RoundedSeconds != 225*60 {
		t.Errorf("total = %+v", out.Total)
	}
	if d := out.Days[0]; d.Day != "2025-12-01" || d.Weekday != "Mon" || d.Weekend || d.Holiday != "" {
		t.Errorf("first day = %+v", d)
	}
	if d := out.Days[24]; d.Day != "2025-12-25" || d.Holiday != "Weihnachten" || d.RawSeconds != 0 || len(d.Categories) != 0 {
		t.Errorf("Christmas = %+v", d)
	}

	out = decode[WorktimeJSON](t, runActivity(t, e, db, "--worktime", "--range", "this-week", "--json").stdout)
	if c, ok := cellOf(out, "2026-01-08", "alpha"); !ok || c.RawSeconds != 6*60 || c.RoundedSeconds != 15*60 {
		t.Errorf("2026-01-08 alpha = %+v (ok %v)", c, ok)
	}
	if len(out.Days) != 7 || out.Total.Days != 3 || out.Total.RoundedSeconds != 45*60 {
		t.Errorf("this week = %d days, total %+v", len(out.Days), out.Total)
	}
}

// TestStatsWorktimeDayStart checks the day rule on A5 (2025-12-31 23:40 and
// 2026-01-01 00:05): one day with the default 04:00 (the 25m gap is above
// the cap, 3m + 3m), two days at midnight (3m each, the second on Neujahr).
func TestStatsWorktimeDayStart(t *testing.T) {
	e := newTestEnv(t)
	e.now = activityNow
	_, db := fixturedb.Activity(t)
	args := []string{"--worktime", "--since", "2025-12-31", "--until", "2026-01-01", "--json"}
	out := decode[WorktimeJSON](t, runActivity(t, e, db, args...).stdout)
	if len(out.Days) != 2 || out.Days[0].RawSeconds != 6*60 || out.Days[1].RawSeconds != 0 || out.Days[1].Holiday != "Neujahr" {
		t.Errorf("04:00: days = %+v", out.Days)
	}
	appendConfig(t, e, "\n[worktime]\nmax_gap = \"30m\"\n")
	out = decode[WorktimeJSON](t, runActivity(t, e, db, args...).stdout)
	if out.Days[0].RawSeconds != 28*60 || out.Days[0].RoundedSeconds != 30*60 {
		t.Errorf("max_gap 30m: day 0 = %+v", out.Days[0])
	}

	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append([]byte("day_start = \"00:00\"\n"), cfg...), 0o644); err != nil {
		t.Fatal(err)
	}
	out = decode[WorktimeJSON](t, runActivity(t, e, db, args...).stdout)
	if out.DayStart != "00:00" || len(out.Days) != 2 || out.Days[0].RawSeconds != 3*60 || out.Days[1].RawSeconds != 3*60 {
		t.Errorf("midnight: days = %+v", out.Days)
	}
}

// TestStatsWorktimeConfig runs the rounding and category settings through
// the command on 2025-12-18 (alpha 33m) and 2025-12-16 (gamma 3m).
func TestStatsWorktimeConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		day     string
		cat     string
		raw     int
		rounded int
	}{
		{"default", "", "2025-12-18", "alpha", 33 * 60, 45 * 60},
		{"down", "\n[worktime]\nrounding = \"down\"\n", "2025-12-18", "alpha", 33 * 60, 30 * 60},
		{"nearest", "\n[worktime]\nrounding = \"nearest\"\n", "2025-12-18", "alpha", 33 * 60, 30 * 60},
		{"nearest up", "\n[worktime]\nrounding = \"nearest\"\ngrid = \"10m\"\n", "2025-12-18", "alpha", 33 * 60, 30 * 60},
		{"grid 6m", "\n[worktime]\ngrid = \"6m\"\n", "2025-12-18", "alpha", 33 * 60, 36 * 60},
		{"no grid", "\n[worktime]\ngrid = \"0m\"\n", "2025-12-18", "alpha", 33 * 60, 33 * 60},
		{"min credit 0", "\n[worktime]\nmin_credit = \"0m\"\n", "2025-12-18", "alpha", 30 * 60, 30 * 60},
		{"max gap 10m", "\n[worktime]\nmax_gap = \"10m\"\n", "2025-12-18", "alpha", 9 * 60, 15 * 60},
		{"category by remote", worktimeCategories, "2025-12-18", "example", 33 * 60, 45 * 60},
		{"catch-all for the directory", worktimeCategories, "2025-12-16", "sonstiges", 3 * 60, 15 * 60},
		{"first rule wins", "\n[[worktime.category]]\nname = \"first\"\nmatch = \"alpha\"\n[[worktime.category]]\nname = \"second\"\nmatch = \"*alpha*\"\n", "2025-12-18", "first", 33 * 60, 45 * 60},
		{"by git root", "\n[[worktime.category]]\nname = \"root\"\nmatch = \"/w/al*\"\n", "2025-12-18", "root", 33 * 60, 45 * 60},
		{"by name", "\n[[worktime.category]]\nname = \"n\"\nmatch = [\"nomatch\", \"alpha\"]\n", "2025-12-18", "n", 33 * 60, 45 * 60},
		{"unmatched keeps its name", "\n[[worktime.category]]\nname = \"n\"\nmatch = \"beta\"\n", "2025-12-18", "alpha", 33 * 60, 45 * 60},
	}
	_, db := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.now = activityNow
			appendConfig(t, e, tt.config)
			out := decode[WorktimeJSON](t, runActivity(t, e, db, "--worktime", "--month", "2025-12", "--json").stdout)
			c, ok := cellOf(out, tt.day, tt.cat)
			if !ok || c.RawSeconds != tt.raw || c.RoundedSeconds != tt.rounded {
				t.Errorf("%s %s = %+v (ok %v); want raw %d rounded %d", tt.day, tt.cat, c, ok, tt.raw, tt.rounded)
			}
		})
	}
}

// TestStatsWorktimeScope checks the rounding scope through the command:
// with scope project and one category for everything, the rounded time of
// every cell is the sum of its rounded projects (the fixture has one
// project per day, so the pure package covers the mixed case).
func TestStatsWorktimeScope(t *testing.T) {
	_, db := fixturedb.Activity(t)
	e := newTestEnv(t)
	e.now = activityNow
	appendConfig(t, e, "\n[worktime]\nscope = \"project\"\n[[worktime.category]]\nname = \"all\"\nmatch = \"*\"\n")
	out := decode[WorktimeJSON](t, runActivity(t, e, db, "--worktime", "--month", "2025-12", "--json").stdout)
	if out.RoundingScope != "project" || len(out.Categories) != 1 || out.Categories[0].Category != "all" {
		t.Fatalf("scope = %q, categories = %+v", out.RoundingScope, out.Categories)
	}
	// Twelve active days with one project each: the same 3h45m as with
	// scope category (see TestStatsWorktimeValues).
	if out.Total.RoundedSeconds != 225*60 || out.Total.Days != 12 {
		t.Errorf("total = %+v", out.Total)
	}
	for _, d := range out.Days {
		for _, c := range d.Categories {
			sum := 0
			for _, p := range c.Projects {
				sum += p.RoundedSeconds
			}
			if sum != c.RoundedSeconds {
				t.Errorf("%s: rounded %d != sum of projects %d", d.Day, c.RoundedSeconds, sum)
			}
		}
	}
}

func TestStatsWorktimeErrors(t *testing.T) {
	e := newTestEnv(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad range", []string{"--worktime", "--range", "yesterday"}, "--range"},
		{"sessions without worktime", []string{"--sessions"}, "--worktime"},
		{"summary without worktime", []string{"--summary"}, "--worktime"},
		{"summary with workday", []string{"--workday", "--summary"}, "--worktime"},
		{"worktime and workday", []string{"--worktime", "--workday"}, "--workday"},
		{"worktime and period", []string{"--worktime", "--period", "week"}, "--period"},
		{"worktime and by", []string{"--worktime", "--by", "project"}, "--by"},
		{"worktime and top", []string{"--worktime", "--top", "3"}, "--top"},
		{"worktime and all events", []string{"--worktime", "--all-events"}, "--all-events"},
		{"range and since", []string{"--worktime", "--range", "this-week", "--since", "2026-01-01"}, "either"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(t, "", append([]string{"stats"}, tt.args...)...)
			if r.code == 0 || !strings.Contains(r.stderr, tt.want) {
				t.Errorf("%v = exit %d, stderr %q; want failure mentioning %q", tt.args, r.code, r.stderr, tt.want)
			}
		})
	}
	// The header names the rounding of the config.
	e.mustRun(t, "ingest", "--all")
	appendConfig(t, e, "\n[worktime]\nrounding = \"nearest\"\ngrid = \"6m\"\nscope = \"project\"\n")
	if r := e.run(t, "", "stats", "--worktime"); !strings.Contains(r.stdout, "rounded to the nearest 6m per project and day") {
		t.Errorf("header does not name the rounding:\n%s", r.stdout)
	}
	// A bad [worktime] table fails this command only, as a config error.
	e = newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	appendConfig(t, e, "\n[worktime]\nrounding = \"ceil\"\n")
	r := e.run(t, "", "stats", "--worktime", "--json")
	if r.code != 1 || !strings.Contains(r.stdout, `"kind":"config"`) || !strings.Contains(r.stdout, "worktime.rounding") {
		t.Errorf("bad config: exit %d, stdout %q", r.code, r.stdout)
	}
	if r := e.run(t, "", "stats", "--workday"); r.code != 0 {
		t.Errorf("--workday must not fail on [worktime]: %s", r.stderr)
	}
	if r := e.run(t, "", "search", "files"); r.code != 0 {
		t.Errorf("search must not fail on [worktime]: %s", r.stderr)
	}
	if r := e.run(t, "", "config", "show"); r.code != 1 || !strings.Contains(r.stdout, "worktime.rounding") {
		t.Errorf("config show must report [worktime]: exit %d\n%s", r.code, r.stdout)
	}
}

// TestStatsWorktimeProjects: [[project]] definitions apply before the
// category rules, which see the configured name; two repositories of one
// project are one project row.
func TestStatsWorktimeProjects(t *testing.T) {
	_, db := fixturedb.Activity(t)
	e := newTestEnv(t)
	e.now = activityNow
	appendConfig(t, e, projectDefinitions+"\n[[worktime.category]]\nname = \"kunde\"\nmatch = \"kompass\"\n")
	out := decode[WorktimeJSON](t, runActivity(t, e, db, "--worktime", "--month", "2025-12", "--json").stdout)
	c, ok := cellOf(out, "2025-12-18", "kunde")
	if !ok || c.RawSeconds != 33*60 {
		t.Fatalf("kunde = %+v (ok %v)", c, ok)
	}
	if len(c.Projects) != 1 || c.Projects[0].Project != "kompass" || c.Projects[0].Key != "kompass" {
		t.Errorf("projects = %+v", c.Projects)
	}
	if g, ok := cellOf(out, "2025-12-16", "notizbox"); !ok || len(g.Projects) != 1 || g.Projects[0].Project != "notizbox" {
		t.Errorf("notizbox = %+v (ok %v)", g, ok)
	}
	for _, cat := range out.Categories {
		if cat.Category == "alpha" || cat.Category == "beta" {
			t.Errorf("category %q survived the definitions: %+v", cat.Category, out.Categories)
		}
	}
	e2 := newTestEnv(t)
	appendConfig(t, e2, "\n[[project]]\nname = \"x\"\n")
	if r := runActivity(t, e2, db, "--worktime", "--month", "2025-12"); r.code == 0 || !strings.Contains(r.stderr, "project[0]") {
		t.Errorf("broken definition: exit %d, stderr %q", r.code, r.stderr)
	}
}

// demoCategories maps two of the demo archive's projects to categories; the
// others (wetterwarte, fahrtenbuch, …) have none.
const demoCategories = `
[[worktime.category]]
name = "kompass"
match = ["https://git.invalid/mara/kompass"]

[[worktime.category]]
name = "privat"
match = ["~/dotfiles", "~/notizen"]
`

// TestStatsWorktimeUncategorized is the sum invariant of "uncategorized"
// against the real --json output of the demo archive: a project of a
// category is not listed, every listed project is one without a category,
// its seconds are the sum of its cells over the range, the list is a part
// of the total, and the cells add up to the total.
func TestStatsWorktimeUncategorized(t *testing.T) {
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	appendConfig(t, e, demoCategories)
	r := runActivity(t, e, db, "--worktime", "--month", "2026-10", "--json")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if r.stderr != "" {
		t.Errorf("JSON mode wrote to stderr: %q", r.stderr)
	}
	out := decode[WorktimeJSON](t, r.stdout)
	perProject := map[string]int{} // project key → raw seconds over the range
	catOf := map[string]string{}
	var cells int
	for _, d := range out.Days {
		for _, c := range d.Categories {
			var sum int
			for _, p := range c.Projects {
				perProject[p.Key] += p.RawSeconds
				catOf[p.Key] = c.Category
				sum += p.RawSeconds
			}
			if sum != c.RawSeconds {
				t.Errorf("%s %s: projects %d != cell %d", d.Day, c.Category, sum, c.RawSeconds)
			}
			cells += c.RawSeconds
		}
	}
	if cells != out.Total.RawSeconds {
		t.Errorf("cells add up to %d, total %d", cells, out.Total.RawSeconds)
	}
	if len(out.Uncategorized) == 0 {
		t.Fatal("demo has projects without a category, uncategorized is empty")
	}
	var sum int
	listed := map[string]bool{}
	for _, u := range out.Uncategorized {
		listed[u.Key] = true
		if u.RawSeconds != perProject[u.Key] || u.RawSeconds <= 0 {
			t.Errorf("%s: raw_seconds %d, cells %d", u.Project, u.RawSeconds, perProject[u.Key])
		}
		if catOf[u.Key] != u.Project {
			t.Errorf("%s is listed but sits in category %q", u.Project, catOf[u.Key])
		}
		sum += u.RawSeconds
	}
	for key, cat := range catOf {
		if (cat == "kompass" || cat == "privat") && listed[key] {
			t.Errorf("%s has the category %s and is listed", key, cat)
		}
		if cat != "kompass" && cat != "privat" && !listed[key] {
			t.Errorf("%s has no category and is not listed", key)
		}
	}
	if sum > out.Total.RawSeconds || sum == out.Total.RawSeconds {
		t.Errorf("uncategorized %d vs total %d: want a proper part", sum, out.Total.RawSeconds)
	}
	for i := 1; i < len(out.Uncategorized); i++ {
		if out.Uncategorized[i-1].RawSeconds < out.Uncategorized[i].RawSeconds {
			t.Errorf("not sorted by seconds: %+v", out.Uncategorized)
		}
	}
}

// TestStatsWorktimeUncategorizedText checks the hint of the table output:
// on stderr, once, with the projects and their time; no hint without
// uncategorized projects and none when no category is configured at all.
func TestStatsWorktimeUncategorizedText(t *testing.T) {
	_, db := fixturedb.Demo(t)
	tests := []struct {
		name, config string
		want         []string // substrings of stderr; none: stderr empty
	}{
		{"projects without a category", demoCategories, []string{"without a category", "wetterwarte", "fahrtenbuch"}},
		{"no categories configured", "", nil},
		{"every project categorized", demoCategories + "\n[[worktime.category]]\nname = \"rest\"\nmatch = \"*\"\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.now = demo.Now
			e.home = demo.Home
			appendConfig(t, e, tt.config)
			r := runActivity(t, e, db, "--worktime", "--month", "2026-10")
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			if strings.Contains(r.stdout, "without a category") {
				t.Errorf("hint on stdout:\n%s", r.stdout)
			}
			if tt.want == nil && r.stderr != "" {
				t.Errorf("stderr = %q, want empty", r.stderr)
			}
			for _, w := range tt.want {
				if !strings.Contains(r.stderr, w) {
					t.Errorf("stderr %q lacks %q", r.stderr, w)
				}
			}
			if tt.want != nil && strings.Count(r.stderr, "without a category") != 1 {
				t.Errorf("hint not exactly once: %q", r.stderr)
			}
		})
	}
}
