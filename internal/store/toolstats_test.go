package store

import (
	"fmt"
	"slices"
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

func toolEv(id, sess, at, tool, input string) model.Event {
	e := ev(id, sess, at, model.ActorAgent, model.KindToolUse, "assistant", input)
	e.ToolName = tool
	return e
}

// seedTools writes tool_use events in three sessions (one with a subagent)
// plus noise that must not be counted: a tool_result and a message that
// carry a tool name or the same text.
func seedTools(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:p1", Harness: "claude", NativeID: "p1", Cwd: "/w/app", GitRoot: "/w/app",
			StartedAt: ts("2026-10-31T22:00:00Z"), EndedAt: ts("2026-10-31T23:30:00Z")},
		{ID: "claude:p1sub", Harness: "claude", NativeID: "p1sub", ParentID: "claude:p1", Cwd: "/w/app/sub",
			StartedAt: ts("2026-10-31T22:05:00Z"), EndedAt: ts("2026-10-31T22:06:00Z")},
		{ID: "claude:p2", Harness: "claude", NativeID: "p2", Cwd: "/w/docs",
			StartedAt: ts("2026-11-01T12:00:00Z"), EndedAt: ts("2026-11-01T12:30:00Z")},
	}
	res := ev("claude:p1-r", "claude:p1", "2026-10-31T22:01:30Z", model.ActorTool, model.KindToolResult, "tool", "/w/app/a.go")
	res.ToolName = "Edit"
	evs := []model.Event{
		// 2026-10-31 22:xx UTC is 23:xx in Berlin (still Oct 31); 23:30Z is Nov 1 in Berlin.
		toolEv("claude:p1-1", "claude:p1", "2026-10-31T22:01:00Z", "Bash", "ls /w/app/src"),
		toolEv("claude:p1-2", "claude:p1", "2026-10-31T22:02:00Z", "Edit", "/w/app/a.go"),
		toolEv("claude:p1-3", "claude:p1", "2026-10-31T23:30:00Z", "Edit", "/w/app/b.go"),
		res,
		ev("claude:p1-m", "claude:p1", "2026-10-31T22:03:00Z", model.ActorAgent, model.KindMessage, "assistant", "/w/app/a.go"),
		toolEv("claude:p1s-1", "claude:p1sub", "2026-10-31T22:05:00Z", "Bash", "git -C /w/app status"),
		toolEv("claude:p2-1", "claude:p2", "2026-11-01T12:00:00Z", "Read", "/w/docs/readme.md"),
		toolEv("claude:p2-2", "claude:p2", "2026-11-01T12:01:00Z", "Bash", "cat /w/docs/readme.md"),
		toolEv("claude:p2-3", "claude:p2", "2026-11-01T12:02:00Z", "Bash", "echo hi"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func fmtTools(c []ToolCount) []string {
	var out []string
	for _, x := range c {
		out = append(out, fmt.Sprintf("%s:%d", x.Tool, x.Count))
	}
	return out
}

func TestToolStats(t *testing.T) {
	s, _ := openTestStore(t)
	seedTools(t, s)
	loc := berlin(t)
	tests := []struct {
		name   string
		f      ToolFilter
		total  int
		byTool []string
		groups map[string]int // key -> count
	}{
		{"All", ToolFilter{}, 7, []string{"Bash:4", "Edit:2", "Read:1"}, nil},
		{"ByDay", ToolFilter{By: ToolByDay, Location: loc}, 7, []string{"Bash:4", "Edit:2", "Read:1"},
			map[string]int{"2026-10-31": 3, "2026-11-01": 4}},
		{"BySession", ToolFilter{By: ToolBySession}, 7, []string{"Bash:4", "Edit:2", "Read:1"},
			map[string]int{"claude:p1": 3, "claude:p1sub": 1, "claude:p2": 3}},
		{"ByRepo", ToolFilter{By: ToolByRepo}, 7, []string{"Bash:4", "Edit:2", "Read:1"},
			map[string]int{"/w/app": 3, "/w/app/sub": 1, "/w/docs": 3}},
		{"ByProjectSubagentToParent", ToolFilter{By: ToolByProject}, 7, []string{"Bash:4", "Edit:2", "Read:1"},
			map[string]int{"/w/app": 4, "/w/docs": 3}},
		{"Since", ToolFilter{Since: ts("2026-11-01T00:00:00Z")}, 3, []string{"Bash:2", "Read:1"}, nil},
		{"PathFileTool", ToolFilter{Path: "/w/app/*.go"}, 2, []string{"Edit:2"}, nil},
		{"PathBashAnywhere", ToolFilter{Path: "/w/docs/*"}, 2, []string{"Bash:1", "Read:1"}, nil},
		{"PathBashGlobAnywhere", ToolFilter{Path: "/w/app*"}, 4, []string{"Bash:2", "Edit:2"}, nil},
		{"PathNoMatch", ToolFilter{Path: "/nowhere"}, 0, nil, nil},
		{"PathByDay", ToolFilter{Path: "/w/app*", By: ToolByDay, Location: loc}, 4, []string{"Bash:2", "Edit:2"},
			map[string]int{"2026-10-31": 3, "2026-11-01": 1}},
		{"TopFoldsIntoOther", ToolFilter{By: ToolBySession, TopN: 1}, 7, []string{"Bash:4", "Edit:2", "Read:1"},
			map[string]int{"claude:p1": 3, OtherKey: 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ToolStats(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if got.Total != tt.total {
				t.Errorf("total = %d, want %d", got.Total, tt.total)
			}
			if g := fmtTools(got.ByTool); !slices.Equal(g, tt.byTool) {
				t.Errorf("by tool = %v, want %v", g, tt.byTool)
			}
			sum := 0
			for _, c := range got.ByTool {
				sum += c.Count
			}
			if sum != got.Total {
				t.Errorf("tools sum to %d, total %d", sum, got.Total)
			}
			if tt.f.By == ToolNoGroup {
				if len(got.Groups) != 0 {
					t.Errorf("groups without By: %v", got.Groups)
				}
				return
			}
			gs, gsum := map[string]int{}, 0
			for _, g := range got.Groups {
				gs[g.Key] = g.Count
				gsum += g.Count
				inner := 0
				for _, c := range g.ByTool {
					inner += c.Count
				}
				if inner != g.Count {
					t.Errorf("group %s: tools sum to %d, count %d", g.Key, inner, g.Count)
				}
			}
			if gsum != got.Total {
				t.Errorf("groups sum to %d, total %d", gsum, got.Total)
			}
			if tt.groups != nil && fmt.Sprint(gs) != fmt.Sprint(tt.groups) {
				t.Errorf("groups = %v, want %v", gs, tt.groups)
			}
		})
	}
}

func TestToolStatsProjectMapper(t *testing.T) {
	s, _ := openTestStore(t)
	seedTools(t, s)
	got, err := s.ToolStats(ctx, ToolFilter{By: ToolByProject, Project: func(key string, _ bool, _ string) string {
		return "all"
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || got.Groups[0].Key != "all" || got.Groups[0].Count != 7 {
		t.Errorf("groups = %+v", got.Groups)
	}
}

func TestParseToolGroup(t *testing.T) {
	for _, n := range []string{"day", "session", "repo", "project"} {
		g, err := ParseToolGroup(n)
		if err != nil || g.String() != n {
			t.Errorf("ParseToolGroup(%q) = %v, %v", n, g, err)
		}
	}
	if _, err := ParseToolGroup("agent"); err == nil {
		t.Error("agent accepted")
	}
}

// TestToolStatsTopWithoutGroups: --top without a grouping folds by_tool.
func TestToolStatsTopWithoutGroups(t *testing.T) {
	s, _ := openTestStore(t)
	seedTools(t, s)
	tests := []struct {
		name   string
		f      ToolFilter
		byTool []string
	}{
		{"One", ToolFilter{TopN: 1}, []string{"Bash:4", "other:3"}},
		{"Two", ToolFilter{TopN: 2}, []string{"Bash:4", "Edit:2", "other:1"}},
		{"ExactlyAll", ToolFilter{TopN: 3}, []string{"Bash:4", "Edit:2", "Read:1"}},
		{"MoreThanTools", ToolFilter{TopN: 10}, []string{"Bash:4", "Edit:2", "Read:1"}},
		{"WithPath", ToolFilter{TopN: 1, Path: "/w/app*"}, []string{"Bash:2", "other:2"}},
		{"Nothing", ToolFilter{TopN: 1, Path: "/nowhere"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ToolStats(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if g := fmtTools(got.ByTool); !slices.Equal(g, tt.byTool) {
				t.Errorf("by tool = %v, want %v", g, tt.byTool)
			}
			sum := 0
			for _, c := range got.ByTool {
				sum += c.Count
			}
			if sum != got.Total {
				t.Errorf("by tool sums to %d, total %d", sum, got.Total)
			}
			if len(got.Groups) != 0 {
				t.Errorf("groups without By: %v", got.Groups)
			}
		})
	}
}

// TestToolStatsTopWithGroupsKeepsTools: with a grouping --top folds groups,
// not the tools of by_tool.
func TestToolStatsTopWithGroupsKeepsTools(t *testing.T) {
	s, _ := openTestStore(t)
	seedTools(t, s)
	got, err := s.ToolStats(ctx, ToolFilter{By: ToolBySession, TopN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if g := fmtTools(got.ByTool); !slices.Equal(g, []string{"Bash:4", "Edit:2", "Read:1"}) {
		t.Errorf("by tool = %v", g)
	}
}

// seedToolMessages adds calls that carry a path in their text without
// working on a file: a message to another session, a sub-agent prompt, a
// web fetch and an MCP tool of another server.
func seedToolMessages(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{{ID: "claude:p3", Harness: "claude", NativeID: "p3", Cwd: "/w/app", GitRoot: "/w/app",
		StartedAt: ts("2026-11-01T13:00:00Z"), EndedAt: ts("2026-11-01T13:30:00Z")}}
	evs := []model.Event{
		toolEv("claude:p3-1", "claude:p3", "2026-11-01T13:00:00Z", "SendMessage", "please look at /w/app/a.go"),
		toolEv("claude:p3-2", "claude:p3", "2026-11-01T13:01:00Z", "Task", "refactor /w/app/b.go"),
		toolEv("claude:p3-3", "claude:p3", "2026-11-01T13:02:00Z", "WebFetch", "https://example.invalid/w/app/a.go"),
		toolEv("claude:p3-4", "claude:p3", "2026-11-01T13:03:00Z", "mcp__notes__post", "/w/app/a.go"),
		toolEv("claude:p3-5", "claude:p3", "2026-11-01T13:04:00Z", "Grep", "/w/app/a.go"),
		toolEv("claude:p3-6", "claude:p3", "2026-11-01T13:05:00Z", "Glob", "/w/app/*.go"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/b.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func TestToolStatsPathScope(t *testing.T) {
	s, _ := openTestStore(t)
	seedTools(t, s)
	seedToolMessages(t, s) // 13 tool_use events in all
	loc := berlin(t)
	tests := []struct {
		name   string
		f      ToolFilter
		total  int
		byTool []string
	}{
		// Bash matches anywhere, the other tools as a whole.
		{"DefaultIsPaths", ToolFilter{Path: "*/w/app/*"}, 5, []string{"Edit:2", "Bash:1", "Glob:1", "Grep:1"}},
		{"ExplicitPaths", ToolFilter{Path: "*/w/app/*", PathIn: PathScopePaths}, 5, []string{"Edit:2", "Bash:1", "Glob:1", "Grep:1"}},
		{"All", ToolFilter{Path: "*/w/app/*", PathIn: PathScopeAll}, 9,
			[]string{"Edit:2", "Bash:1", "Glob:1", "Grep:1", "SendMessage:1", "Task:1", "WebFetch:1", "mcp__notes__post:1"}},
		{"WholePathOfOtherToolInAll", ToolFilter{Path: "/w/app/a.go", PathIn: PathScopeAll}, 3, []string{"Edit:1", "Grep:1", "mcp__notes__post:1"}},
		{"WholePathOfOtherToolInPaths", ToolFilter{Path: "/w/app/a.go"}, 2, []string{"Edit:1", "Grep:1"}},
		{"NothingMatches", ToolFilter{Path: "/nowhere", PathIn: PathScopeAll}, 0, nil},
		{"ScopeWithoutPathCountsEverything", ToolFilter{PathIn: PathScopePaths}, 13, []string{"Bash:4", "Edit:2", "Glob:1", "Grep:1", "Read:1", "SendMessage:1", "Task:1", "WebFetch:1", "mcp__notes__post:1"}},
	}
	for _, tt := range tests {
		for _, by := range []ToolGroup{ToolNoGroup, ToolByDay, ToolBySession, ToolByProject} {
			t.Run(tt.name+"/"+by.String(), func(t *testing.T) {
				f := tt.f
				f.By, f.Location = by, loc
				got, err := s.ToolStats(ctx, f)
				if err != nil {
					t.Fatal(err)
				}
				if got.Total != tt.total {
					t.Errorf("total = %d, want %d", got.Total, tt.total)
				}
				if g := fmtTools(got.ByTool); !slices.Equal(g, tt.byTool) {
					t.Errorf("by tool = %v, want %v", g, tt.byTool)
				}
				sum := 0
				for _, c := range got.ByTool {
					sum += c.Count
				}
				gsum := 0
				for _, g := range got.Groups {
					gsum += g.Count
				}
				if sum != got.Total || (by != ToolNoGroup && gsum != got.Total) {
					t.Errorf("tools sum to %d, groups to %d, total %d", sum, gsum, got.Total)
				}
			})
		}
	}
}

func TestPathToolsAreDocumented(t *testing.T) {
	for _, n := range []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"} {
		if len(PathTools[n]) == 0 {
			t.Errorf("%s has no path fields", n)
		}
	}
	for _, n := range []string{"SendMessage", "Task", "WebFetch", "Skill"} {
		if _, ok := PathTools[n]; ok {
			t.Errorf("%s must not be a path tool", n)
		}
	}
	for _, in := range []string{"paths", "all"} {
		if s, err := ParsePathScope(in); err != nil || s.String() != in {
			t.Errorf("ParsePathScope(%q) = %v, %v", in, s, err)
		}
	}
	if _, err := ParsePathScope("every"); err == nil {
		t.Error("every accepted")
	}
}
