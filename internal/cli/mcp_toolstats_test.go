package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

// TestMCPToolStatsPagesAddUp is the invariant of #230 for stats tools=true:
// over the demo archive with a bound that forces pages, every page has the
// same total and by_tool, the groups of all pages sum to the total, each
// group's tools to its count, and the pages together are the CLI JSON.
func TestMCPToolStatsPagesAddUp(t *testing.T) {
	const since, until = "2026-01-01", "2026-12-31"
	for _, tc := range []struct {
		by    string
		bound int
	}{
		{"session", 3 << 10},
		{"day", 2 << 10},
		{"repo", 1 << 10},
		{"project", 1 << 10},
	} {
		t.Run(tc.by, func(t *testing.T) {
			p := newPagedDemo(t, tc.bound)
			want := p.cli("stats", "--tools", "--by="+tc.by, "--since="+since, "--until="+until)
			var whole ToolStatsJSON
			if err := json.Unmarshal([]byte(want), &whole); err != nil {
				t.Fatal(err)
			}
			if len(want) <= tc.bound || whole.Total == 0 {
				t.Skipf("%d bytes, %d calls: no pages to test", len(want), whole.Total)
			}
			pages, hints := p.crawl("stats", map[string]any{"tools": true, "by": tc.by, "since": since, "until": until})
			if len(pages) < 2 {
				t.Fatalf("%d pages for %d bytes", len(pages), len(want))
			}
			groups, sum := 0, 0
			for i, pg := range pages {
				if len(pg) > tc.bound || hints[i] == nil || hints[i].Truncated || hints[i].Page == nil || hints[i].Page.Unit != "groups" {
					t.Fatalf("page %d: %d bytes (bound %d), hint %+v", i+1, len(pg), tc.bound, hints[i])
				}
				var w ToolStatsJSON
				if err := json.Unmarshal([]byte(pg), &w); err != nil {
					t.Fatal(err)
				}
				if w.Total != whole.Total || fmt.Sprint(w.ByTool) != fmt.Sprint(whole.ByTool) {
					t.Errorf("page %d: total %d, by_tool %v; whole %d, %v", i+1, w.Total, w.ByTool, whole.Total, whole.ByTool)
				}
				for _, g := range w.Groups {
					inner := 0
					for _, c := range g.ByTool {
						inner += c.Count
					}
					if inner != g.Count {
						t.Errorf("group %q: tools sum to %d, count %d", g.Key, inner, g.Count)
					}
					sum += g.Count
					groups++
				}
			}
			if groups != len(whole.Groups) || sum != whole.Total {
				t.Errorf("%d of %d groups, counts sum to %d, total %d", groups, len(whole.Groups), sum, whole.Total)
			}
			if got := reassemble(t, pages, "groups"); got != want {
				t.Errorf("pages do not add up to the CLI JSON (%d vs %d bytes)", len(got), len(want))
			}
		})
	}
}

// TestMCPToolStatsMatchesCLI: tools, by, path and top reach the command.
func TestMCPToolStatsMatchesCLI(t *testing.T) {
	p := newPagedDemo(t, 16<<20)
	for _, tc := range []struct {
		name string
		args map[string]any
		cli  []string
	}{
		{"plain", map[string]any{"tools": true, "since": "2026-10-01", "until": "2026-10-31"}, []string{"stats", "--tools", "--since=2026-10-01", "--until=2026-10-31"}},
		{"by day with path", map[string]any{"tools": true, "by": "day", "path": "*go*", "since": "2026-10-01", "until": "2026-10-31"}, []string{"stats", "--tools", "--by=day", "--path=*go*", "--since=2026-10-01", "--until=2026-10-31"}},
		{"by session top", map[string]any{"tools": true, "by": "session", "top": 2, "since": "2026-10-01", "until": "2026-10-31"}, []string{"stats", "--tools", "--by=session", "--top=2", "--since=2026-10-01", "--until=2026-10-31"}},
		{"path scope all", map[string]any{"tools": true, "path": "*go*", "path_in": "all", "since": "2026-10-01", "until": "2026-10-31"}, []string{"stats", "--tools", "--path=*go*", "--path-in=all", "--since=2026-10-01", "--until=2026-10-31"}},
		{"path scope paths", map[string]any{"tools": true, "path": "*go*", "path_in": "paths", "by": "day", "since": "2026-10-01", "until": "2026-10-31"}, []string{"stats", "--tools", "--path=*go*", "--path-in=paths", "--by=day", "--since=2026-10-01", "--until=2026-10-31"}},
		{"top without by", map[string]any{"tools": true, "top": 1, "since": "2026-10-01", "until": "2026-10-31"}, []string{"stats", "--tools", "--top=1", "--since=2026-10-01", "--until=2026-10-31"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, isErr := p.call("stats", tc.args)
			if isErr {
				t.Fatalf("isError: %s", got)
			}
			var b bytes.Buffer
			if err := json.Compact(&b, []byte(got)); err != nil {
				t.Fatal(err)
			}
			if want, got := p.cli(tc.cli...), b.String(); got != want {
				t.Errorf("tool differs from CLI\n tool: %.300s\n  cli: %.300s", got, want)
			}
		})
	}
}

// TestMCPToolStatsTopSumsToTotal: over MCP, tools=true with top and without
// by gives by_tool with "other" that sums to total, like the CLI.
func TestMCPToolStatsTopSumsToTotal(t *testing.T) {
	p := newPagedDemo(t, 16<<20)
	for _, top := range []int{1, 2, 50} {
		got, _, isErr := p.call("stats", map[string]any{"tools": true, "top": top, "since": "2026-01-01", "until": "2026-12-31"})
		if isErr {
			t.Fatalf("top %d: %s", top, got)
		}
		var w ToolStatsJSON
		if err := json.Unmarshal([]byte(got), &w); err != nil {
			t.Fatal(err)
		}
		sum := 0
		for _, c := range w.ByTool {
			sum += c.Count
		}
		if w.Total == 0 || sum != w.Total || len(w.ByTool) > top+1 || len(w.Groups) != 0 {
			t.Errorf("top %d: by_tool %v sums to %d, total %d", top, w.ByTool, sum, w.Total)
		}
	}
}

// TestMCPToolStatsPathScopePagesAddUp: tools, path and path_in together,
// paged: groups of all pages sum to total on every page's total.
func TestMCPToolStatsPathScopePagesAddUp(t *testing.T) {
	p := newPagedDemo(t, 2<<10)
	args := map[string]any{"tools": true, "by": "session", "path": "*", "path_in": "all", "since": "2026-01-01", "until": "2026-12-31"}
	pages, hints := p.crawl("stats", args)
	if len(pages) < 2 {
		t.Fatalf("%d pages", len(pages))
	}
	sum, total := 0, -1
	for i, pg := range pages {
		if len(pg) > 2<<10 || hints[i] == nil || hints[i].Truncated {
			t.Fatalf("page %d: %d bytes, hint %+v", i+1, len(pg), hints[i])
		}
		var w ToolStatsJSON
		if err := json.Unmarshal([]byte(pg), &w); err != nil {
			t.Fatal(err)
		}
		if total >= 0 && w.Total != total {
			t.Errorf("page %d: total %d, first page %d", i+1, w.Total, total)
		}
		total = w.Total
		for _, g := range w.Groups {
			sum += g.Count
		}
		if w.PathIn != "all" {
			t.Errorf("page %d: path_in %q", i+1, w.PathIn)
		}
	}
	if sum != total || total == 0 {
		t.Errorf("groups sum to %d, total %d", sum, total)
	}
}
