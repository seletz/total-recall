package tomledit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `# total-recall configuration
# Precedence: flag > environment variable > this file > built-in default.

# Path of the archive database.
# db = "~/Library/Application Support/total-recall/archive.db"
timezone = "Europe/Berlin" # for evaluations
log = ""

[backup]
dir = "~/Documents/total-recall"   # target
keep_daily = 7

[redact]
enabled = true
extra_patterns = [
  "foo",
  "bar",
]

[harness.claude]
enabled = true
# Default: $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects.
# projects_dir = "~/.claude/projects"
skip_record_types = []

[harness.claude.actors]
peer_prefixes = []

[tui]
view = "search"
reload = "5s"

# Names for the account IDs.
# [accounts]
# "aaaaaaaa-0000-4000-8000-000000000001" = "work"

[keys]
# Key bindings of the TUI: action = key or list of keys.
# global_quit = ["q", "ctrl+q"]
`

func TestSet(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
		want  []string // lines that must appear
		gone  []string // lines that must not appear
	}{
		{"replace keeps trailing comment", "timezone", "UTC",
			[]string{`timezone = "UTC" # for evaluations`}, []string{`"Europe/Berlin"`}},
		{"replace in table keeps comment", "backup.dir", "/x",
			[]string{`dir = "/x"   # target`}, []string{`~/Documents`}},
		{"int", "backup.keep_daily", 3, []string{`keep_daily = 3`}, []string{`keep_daily = 7`}},
		{"bool", "redact.enabled", false, []string{`enabled = false`}, nil},
		{"duration", "tui.reload", 10 * time.Second, []string{`reload = "10s"`}, []string{`"5s"`}},
		{"multi-line array collapses", "redact.extra_patterns", []string{"x"},
			[]string{`extra_patterns = ["x"]`}, []string{`"foo",`, `"bar",`}},
		{"activates commented key at top level", "db", "/db",
			[]string{"# Path of the archive database.\ndb = \"/db\"\ntimezone"}, []string{`# db =`}},
		{"activates commented key in table", "harness.claude.projects_dir", "/p",
			[]string{"\nprojects_dir = \"/p\"\nskip_record_types"}, []string{`# projects_dir`}},
		{"activates commented key with example value", "keys.global_quit", []string{"x"},
			[]string{"\nglobal_quit = [\"x\"]\n"}, []string{`# global_quit`}},
		{"inserts after last key of table", "backup.keep_monthly", 2,
			[]string{"keep_daily = 7\nkeep_monthly = 2\n\n[redact]"}, nil},
		{"inserts into sub-table not parent", "harness.claude.actors.automation_prefixes", []string{"Hourly "},
			[]string{"peer_prefixes = []\nautomation_prefixes = [\"Hourly \"]\n"}, nil},
		{"inserts top-level key after last top-level key", "day_start", "04:00",
			[]string{"log = \"\"\nday_start = \"04:00\"\n\n[backup]"}, nil},
		{"activates commented table header", "accounts.aaaaaaaa-0000-4000-8000-000000000002", "private",
			[]string{"[accounts]\naaaaaaaa-0000-4000-8000-000000000002 = \"private\"\n# \"aaaaaaaa-0000-4000-8000-000000000001\" = \"work\""}, []string{"# [accounts]"}},
		{"appends missing table", "work.hours", "08:00-16:00",
			[]string{"\n[work]\nhours = \"08:00-16:00\"\n"}, nil},
		{"string escapes", "log", `a"b\c`, []string{`log = "a\"b\\c"`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Parse(sample)
			if err := d.Set(tt.key, tt.value); err != nil {
				t.Fatalf("Set: %v", err)
			}
			got := d.String()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
			for _, g := range tt.gone {
				if strings.Contains(got, g) {
					t.Errorf("output still has %q:\n%s", g, got)
				}
			}
			// Everything that is not the changed key is untouched.
			diff, err := Diff(sample, got)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if len(diff) != 1 || diff[0] != tt.key {
				t.Errorf("Diff = %v, want [%s]", diff, tt.key)
			}
		})
	}
}

func TestSetKeepsEverythingElse(t *testing.T) {
	d := Parse(sample)
	if err := d.Set("tui.view", "stats"); err != nil {
		t.Fatal(err)
	}
	got, want := strings.Split(d.String(), "\n"), strings.Split(sample, "\n")
	if len(got) != len(want) {
		t.Fatalf("line count %d, want %d", len(got), len(want))
	}
	for i := range want {
		if want[i] == `view = "search"` {
			if got[i] != `view = "stats"` {
				t.Errorf("line %d = %q", i, got[i])
			}
			continue
		}
		if got[i] != want[i] {
			t.Errorf("line %d changed: %q -> %q", i, want[i], got[i])
		}
	}
}

func TestSetErrors(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
	}{
		{"empty key", "", 1},
		{"unsupported type", "tui.view", struct{}{}},
		{"table array needs SetAt", "worktime.category.name", "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Parse(sample + "\n[[worktime.category]]\nname = \"a\"\n")
			if err := d.Set(tt.key, tt.value); err == nil {
				t.Errorf("Set(%q) succeeded:\n%s", tt.key, d.String())
			}
		})
	}
}

func TestUnset(t *testing.T) {
	d := Parse(sample)
	if err := d.Unset("backup.dir"); err != nil {
		t.Fatal(err)
	}
	got := d.String()
	if !strings.Contains(got, "# dir = \"~/Documents/total-recall\"   # target\n") {
		t.Errorf("line not commented out:\n%s", got)
	}
	if err := d.Unset("redact.extra_patterns"); err != nil {
		t.Fatal(err)
	}
	got = d.String()
	if !strings.Contains(got, "# extra_patterns = [\n#   \"foo\",\n#   \"bar\",\n# ]\n") {
		t.Errorf("multi-line value not commented out:\n%s", got)
	}
	if err := d.Unset("nope.nothing"); err == nil {
		t.Error("Unset of a missing key succeeded")
	}
	diff, err := Diff(sample, got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(diff, ",") != "backup.dir,redact.extra_patterns" {
		t.Errorf("Diff = %v", diff)
	}
}

const withTables = sample + `
[worktime]
grid = "15m"

[[worktime.category]]
name = "kunde-a"
match = ["https://github.com/kunde-a/*"]

[[worktime.category]]
name = "rest"
match = "*"

[git]
resolve = true
`

func TestTableArrays(t *testing.T) {
	d := Parse(withTables)
	if n := d.Count("worktime.category"); n != 2 {
		t.Fatalf("Count = %d", n)
	}
	if err := d.SetAt("worktime.category", 1, "name", "sonstiges"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetAt("worktime.category", 0, "description", "customer"); err != nil {
		t.Fatal(err)
	}
	if err := d.AppendTable("worktime.category", []Pair{{"name", "neu"}, {"match", []string{"~/src/neu"}}}); err != nil {
		t.Fatal(err)
	}
	got := d.String()
	wants := []string{
		"name = \"sonstiges\"\nmatch = \"*\"\n",
		"match = [\"https://github.com/kunde-a/*\"]\ndescription = \"customer\"\n",
		"match = \"*\"\n\n[[worktime.category]]\nname = \"neu\"\nmatch = [\"~/src/neu\"]\n\n[git]",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("output lacks %q:\n%s", w, got)
		}
	}
	if n := d.Count("worktime.category"); n != 3 {
		t.Errorf("Count after append = %d", n)
	}
	if err := d.RemoveTable("worktime.category", 0); err != nil {
		t.Fatal(err)
	}
	got = d.String()
	if strings.Contains(got, "kunde-a") || d.Count("worktime.category") != 2 {
		t.Errorf("first table not removed:\n%s", got)
	}
	if !strings.Contains(got, "grid = \"15m\"\n\n[[worktime.category]]\nname = \"sonstiges\"") {
		t.Errorf("remove left a gap:\n%s", got)
	}
	if err := d.RemoveTable("worktime.category", 5); err == nil {
		t.Error("RemoveTable out of range succeeded")
	}
	if err := d.SetAt("worktime.category", 9, "name", "x"); err == nil {
		t.Error("SetAt out of range succeeded")
	}
}

func TestAppendTableWithoutParent(t *testing.T) {
	d := Parse("timezone = \"UTC\"\n")
	if err := d.AppendTable("project", []Pair{{"name", "a"}, {"color", "#ff0000"}}); err != nil {
		t.Fatal(err)
	}
	want := "timezone = \"UTC\"\n\n[[project]]\nname = \"a\"\ncolor = \"#ff0000\"\n"
	if got := d.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// The second one follows the first.
	if err := d.AppendTable("project", []Pair{{"name", "b"}}); err != nil {
		t.Fatal(err)
	}
	if got := d.String(); got != want+"\n[[project]]\nname = \"b\"\n" {
		t.Errorf("got\n%s", got)
	}
}

func TestAppendTableAfterParent(t *testing.T) {
	d := Parse("[worktime]\ngrid = \"15m\"\n\n[git]\nresolve = true\n")
	if err := d.AppendTable("worktime.category", []Pair{{"name", "a"}}); err != nil {
		t.Fatal(err)
	}
	want := "[worktime]\ngrid = \"15m\"\n\n[[worktime.category]]\nname = \"a\"\n\n[git]\nresolve = true\n"
	if got := d.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		v    any
		want string
	}{
		{"s", `"s"`},
		{true, "true"},
		{42, "42"},
		{int64(7), "7"},
		{2.5, "2.5"},
		{90 * time.Minute, `"1h30m0s"`},
		{[]string{}, "[]"},
		{[]string{"a", "b"}, `["a", "b"]`},
		{Raw("[1, 2]"), "[1, 2]"},
		{"tab\tand\nnewline", `"tab\tand\nnewline"`},
	}
	for _, tt := range tests {
		got, err := Format(tt.v)
		if err != nil {
			t.Errorf("Format(%v): %v", tt.v, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Format(%v) = %s, want %s", tt.v, got, tt.want)
		}
	}
	if _, err := Format(map[string]int{}); err == nil {
		t.Error("Format(map) succeeded")
	}
}

func TestDiff(t *testing.T) {
	a := "x = 1\n[t]\ny = \"a\"\n[[arr]]\nn = 1\n"
	tests := []struct {
		name string
		b    string
		want string
	}{
		{"equal", a, ""},
		{"changed", "x = 2\n[t]\ny = \"a\"\n[[arr]]\nn = 1\n", "x"},
		{"added", a + "[u]\nz = true\n", "u.z"},
		{"removed", "x = 1\n[[arr]]\nn = 1\n", "t.y"},
		{"array of tables", "x = 1\n[t]\ny = \"a\"\n[[arr]]\nn = 2\n", "arr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Diff(a, tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != tt.want {
				t.Errorf("Diff = %v, want %q", got, tt.want)
			}
		})
	}
	if _, err := Diff(a, "x = \n"); err == nil {
		t.Error("Diff of invalid TOML succeeded")
	}
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "config.toml")
	if err := Save(p, "a = 1\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("a backup was written although there was no file")
	}
	if err := Save(p, "a = 2\n"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	bak, _ := os.ReadFile(p + ".bak")
	if string(got) != "a = 2\n" || string(bak) != "a = 1\n" {
		t.Errorf("file %q, bak %q", got, bak)
	}
	if entries, _ := os.ReadDir(filepath.Dir(p)); len(entries) != 2 {
		t.Errorf("leftovers: %v", entries)
	}
}

func TestParseRoundTrip(t *testing.T) {
	for _, text := range []string{"", "a = 1", "a = 1\n", sample, withTables, "x = \"\"\"\nmulti\n\"\"\"\ny = 1\n"} {
		if got := Parse(text).String(); got != text {
			t.Errorf("round trip changed\n%q\nto\n%q", text, got)
		}
	}
}

const withProjects = `timezone = "UTC"

# the projects
[[project]]
name = "kompass"
description = "Wanderkarten"
color = "blue"
match = ["~/src/kompass*"]

[[project]]
name = "wetterwarte"
match = "https://git.invalid/mara/wetterwarte"

[[project]]
name = "fahrtenbuch"
color = "green"
match = "~/src/fahrtenbuch"
[tui]
view = "stats"
`

func TestUnsetAt(t *testing.T) {
	d := Parse(withProjects)
	if err := d.UnsetAt("project", 0, "description"); err != nil {
		t.Fatal(err)
	}
	if got := d.String(); !strings.Contains(got, "name = \"kompass\"\n# description = \"Wanderkarten\"\ncolor = \"blue\"\n") {
		t.Errorf("description not commented out:\n%s", got)
	}
	// A commented-out key is activated again by SetAt.
	if err := d.SetAt("project", 0, "description", "Karten"); err != nil {
		t.Fatal(err)
	}
	if got := d.String(); !strings.Contains(got, "name = \"kompass\"\ndescription = \"Karten\"\ncolor = \"blue\"\n") {
		t.Errorf("description not activated:\n%s", got)
	}
	// A key the table does not set is fine.
	if err := d.UnsetAt("project", 1, "color"); err != nil {
		t.Errorf("UnsetAt of a missing key: %v", err)
	}
	if err := d.UnsetAt("project", 7, "color"); err == nil {
		t.Error("UnsetAt out of range succeeded")
	}
	if d.Count("project") != 3 {
		t.Errorf("Count = %d", d.Count("project"))
	}
}

func TestMoveTable(t *testing.T) {
	tests := []struct {
		name     string
		from, to int
		order    []string
	}{
		{"down one", 0, 1, []string{"wetterwarte", "kompass", "fahrtenbuch"}},
		{"up one", 2, 1, []string{"kompass", "fahrtenbuch", "wetterwarte"}},
		{"to the end", 0, 2, []string{"wetterwarte", "fahrtenbuch", "kompass"}},
		{"to the front", 2, 0, []string{"fahrtenbuch", "kompass", "wetterwarte"}},
		{"same place", 1, 1, []string{"kompass", "wetterwarte", "fahrtenbuch"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Parse(withProjects)
			if err := d.MoveTable("project", tt.from, tt.to); err != nil {
				t.Fatal(err)
			}
			got := d.String()
			var names []string
			for _, l := range strings.Split(got, "\n") {
				if strings.HasPrefix(l, "name = ") {
					names = append(names, strings.Trim(strings.TrimPrefix(l, "name = "), `"`))
				}
			}
			if strings.Join(names, ",") != strings.Join(tt.order, ",") {
				t.Errorf("order = %v, want %v:\n%s", names, tt.order, got)
			}
			// Each table keeps its entries, the rest of the file stays.
			for _, w := range []string{"# the projects\n", "name = \"kompass\"\ndescription = \"Wanderkarten\"\ncolor = \"blue\"\nmatch = [\"~/src/kompass*\"]\n", "name = \"fahrtenbuch\"\ncolor = \"green\"\nmatch = \"~/src/fahrtenbuch\"\n", "\n[tui]\nview = \"stats\"\n", "timezone = \"UTC\"\n"} {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
			if _, err := Diff(withProjects, got); err != nil {
				t.Errorf("invalid TOML after move: %v\n%s", err, got)
			}
			if strings.Contains(got, "\n\n\n") {
				t.Errorf("double blank line:\n%s", got)
			}
			if d.Count("project") != 3 {
				t.Errorf("Count = %d", d.Count("project"))
			}
		})
	}
	d := Parse(withProjects)
	if err := d.MoveTable("project", 0, 3); err == nil {
		t.Error("MoveTable out of range succeeded")
	}
}
