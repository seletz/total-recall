package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// demoToolStats runs stats --tools against the demo archive and decodes the
// real --json output.
func demoToolStats(t *testing.T, e *testEnv, db string, args ...string) ToolStatsJSON {
	t.Helper()
	r := runActivity(t, e, db, append([]string{"--tools", "--json", "--since", "2026-01-01", "--until", "2026-12-31"}, args...)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	var out ToolStatsJSON
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("%v\n%s", err, r.stdout)
	}
	return out
}

// toolUseCount is the ground truth: tool_use events of the archive, by SQL.
func toolUseCount(t *testing.T, e *testEnv, db, where string) int {
	t.Helper()
	r := e.run(t, "", "--db", db, "sql", "--json", "SELECT count(*) AS n FROM events WHERE kind = 'tool_use'"+where)
	if r.code != 0 {
		t.Fatalf("sql: %s", r.stderr)
	}
	var rows []struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("sql output %v: %s", err, r.stdout)
	}
	return rows[0].N
}

// TestStatsToolsInvariants checks "numbers add up" on the real JSON of the
// demo archive: tools sum to the total, groups sum to the total and each
// group's tools to its count, for every grouping, with and without --top and
// --path, and the total equals the tool_use events of the archive.
func TestStatsToolsInvariants(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	want := toolUseCount(t, e, db, "")
	if want == 0 {
		t.Fatal("demo archive has no tool_use events")
	}
	tests := []struct {
		name  string
		args  []string
		total int // -1: not checked against SQL
	}{
		{"Plain", nil, want},
		{"Day", []string{"--by", "day"}, want},
		{"Session", []string{"--by", "session"}, want},
		{"Repo", []string{"--by", "repo"}, want},
		{"Project", []string{"--by", "project"}, want},
		{"ProjectTop2", []string{"--by", "project", "--top", "2"}, want},
		{"SessionTop3", []string{"--by", "session", "--top", "3"}, want},
		{"ToolsTop1", []string{"--top", "1"}, want},
		{"ToolsTop2", []string{"--top", "2"}, want},
		{"ToolsTopMoreThanTools", []string{"--top", "50"}, want},
		{"ToolsTopWithPath", []string{"--top", "1", "--path", "*go*"}, -1},
		{"PathBashAndFiles", []string{"--path", "*.go"}, -1},
		{"PathAllArguments", []string{"--path", "*go*", "--path-in", "all"}, -1},
		{"PathPathsExplicitByProject", []string{"--path", "*go*", "--path-in", "paths", "--by", "project"}, -1},
		{"PathAllBySessionTop", []string{"--path", "*", "--path-in", "all", "--by", "session", "--top", "3"}, want},
		{"PathAllToolsTop1", []string{"--path", "*", "--path-in", "all", "--top", "1"}, want},
		{"PathByDay", []string{"--path", "*go*", "--by", "day"}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := demoToolStats(t, e, db, tt.args...)
			if tt.total >= 0 && got.Total != tt.total {
				t.Errorf("total = %d, want %d (tool_use events)", got.Total, tt.total)
			}
			sum := 0
			for _, c := range got.ByTool {
				sum += c.Count
			}
			if sum != got.Total {
				t.Errorf("by_tool sums to %d, total %d", sum, got.Total)
			}
			if got.By == "" {
				if len(got.Groups) != 0 {
					t.Errorf("groups without --by: %v", got.Groups)
				}
				return
			}
			gsum := 0
			for _, g := range got.Groups {
				gsum += g.Count
				inner := 0
				for _, c := range g.ByTool {
					inner += c.Count
				}
				if inner != g.Count {
					t.Errorf("group %q: tools sum to %d, count %d", g.Key, inner, g.Count)
				}
			}
			if gsum != got.Total {
				t.Errorf("groups sum to %d, total %d", gsum, got.Total)
			}
		})
	}
}

// TestStatsToolsDemoValues pins the shape on the demo archive: known tools,
// the path filter against SQL, and --top folding.
func TestStatsToolsDemoValues(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	all := demoToolStats(t, e, db)
	names := map[string]int{}
	for _, c := range all.ByTool {
		names[c.Tool] = c.Count
	}
	for _, n := range []string{"Bash", "Read", "Edit"} {
		if names[n] == 0 {
			t.Errorf("tool %s missing: %v", n, all.ByTool)
		}
	}
	if all.By != "" || all.Path != "" || all.Timezone == "" {
		t.Errorf("header: by=%q path=%q tz=%q", all.By, all.Path, all.Timezone)
	}
	wantPath := toolUseCount(t, e, db, " AND ((tool_name = 'Bash' AND text GLOB '*go test*') OR (tool_name <> 'Bash' AND text GLOB 'go test*'))")
	got := demoToolStats(t, e, db, "--path", "go test*")
	if wantPath == 0 || got.Total != wantPath || got.Path != "go test*" {
		t.Errorf("path filter total = %d (path %q), want %d", got.Total, got.Path, wantPath)
	}
	top := demoToolStats(t, e, db, "--by", "session", "--top", "2")
	if len(top.Groups) != 3 || top.Groups[2].Key != "other" {
		t.Errorf("top 2 groups = %+v", top.Groups)
	}
}

func TestStatsToolsFlags(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	tests := []struct {
		name string
		args []string
		msg  string
	}{
		{"PathNeedsTools", []string{"--path", "x"}, "--path requires --tools"},
		{"BadBy", []string{"--tools", "--by", "agent"}, "invalid grouping"},
		{"WithPeriod", []string{"--tools", "--period", "week"}, "cannot be combined"},
		{"WithWorkday", []string{"--tools", "--workday"}, "cannot be combined"},
		{"WithTokens", []string{"--tools", "--tokens"}, "cannot be combined"},
		{"PathInNeedsPath", []string{"--tools", "--path-in", "all"}, "--path-in requires --path"},
		{"BadPathIn", []string{"--tools", "--path", "x", "--path-in", "every"}, "invalid path scope"},
		{"PathInNeedsTools", []string{"--path-in", "all"}, "--path-in requires --tools"},
		{"NegativeTop", []string{"--tools", "--top", "-1"}, "--top must not be negative"},
		{"ByDayWithoutToolsKeepsSeriesError", []string{"--by", "day"}, "invalid dimension"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runActivity(t, e, db, tt.args...)
			if r.code == 0 || !strings.Contains(r.stderr+r.stdout, tt.msg) {
				t.Errorf("exit %d, want error containing %q: %s%s", r.code, tt.msg, r.stderr, r.stdout)
			}
		})
	}
}

func TestStatsToolsTable(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	r := runActivity(t, e, db, "--tools", "--since", "2026-10-01", "--until", "2026-10-31")
	if r.code != 0 || !strings.Contains(r.stdout, "TOOL") || !strings.Contains(r.stdout, "Bash") || !strings.Contains(r.stdout, TotalLabel) {
		t.Errorf("table: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = runActivity(t, e, db, "--tools", "--by", "day", "--since", "2026-10-01", "--until", "2026-10-31")
	if r.code != 0 || !strings.Contains(r.stdout, "KEY") || !strings.Contains(r.stdout, "2026-10-") {
		t.Errorf("grouped table: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

// TestStatsToolsTopWithoutBy: --top N keeps the N most used tools in by_tool
// and folds the rest into "other"; a larger N changes nothing.
func TestStatsToolsTopWithoutBy(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	all := demoToolStats(t, e, db)
	if len(all.ByTool) < 3 {
		t.Fatalf("demo archive has %d tools", len(all.ByTool))
	}
	one := demoToolStats(t, e, db, "--top", "1")
	if len(one.ByTool) != 2 || one.ByTool[0] != all.ByTool[0] || one.ByTool[1].Tool != "other" ||
		one.ByTool[1].Count != all.Total-all.ByTool[0].Count || len(one.Groups) != 0 || one.By != "" {
		t.Errorf("top 1 = %+v (all %+v)", one.ByTool, all.ByTool)
	}
	big := demoToolStats(t, e, db, "--top", "50")
	if fmt.Sprint(big.ByTool) != fmt.Sprint(all.ByTool) {
		t.Errorf("top 50 = %v, want %v (no other)", big.ByTool, all.ByTool)
	}
	exact := demoToolStats(t, e, db, "--top", strconv.Itoa(len(all.ByTool)))
	if fmt.Sprint(exact.ByTool) != fmt.Sprint(all.ByTool) {
		t.Errorf("top %d = %v, want no other", len(all.ByTool), exact.ByTool)
	}
}

// TestStatsToolsPathScope: the default checks only the path and command
// tools, "all" every call; "*" matches every argument, so with "all" it is
// every tool_use of the archive and the default is a part of it. Both add up.
func TestStatsToolsPathScope(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	want := toolUseCount(t, e, db, "")
	all := demoToolStats(t, e, db, "--path", "*", "--path-in", "all")
	if all.Total != want || all.PathIn != "all" || all.Path != "*" {
		t.Errorf("all: total %d (path %q, path_in %q), want %d", all.Total, all.Path, all.PathIn, want)
	}
	paths := demoToolStats(t, e, db, "--path", "*")
	if paths.PathIn != "paths" || paths.Total == 0 || paths.Total > all.Total {
		t.Errorf("paths: total %d (path_in %q), all %d", paths.Total, paths.PathIn, all.Total)
	}
	for _, c := range paths.ByTool {
		if !slices.Contains([]string{"Bash", "Read", "Edit", "MultiEdit", "Write", "Glob", "Grep", "LS"}, c.Tool) {
			t.Errorf("tool %s counted by the default scope", c.Tool)
		}
	}
	if none := demoToolStats(t, e, db); none.PathIn != "" || none.Total != want {
		t.Errorf("without --path: path_in %q, total %d, want %d", none.PathIn, none.Total, want)
	}
}
