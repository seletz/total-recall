package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/gitinfo"
	tf "github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// summaryFixture writes one morning (2026-11-03, local time) in two
// repositories: kompass with a titled session whose messages name #31 and
// PR #32 (a tool result and the agent's reply name #999 and #888, which
// must not count), a session titled "Konfig (#21)", an untitled session
// with refs and an issue URL; wetterwarte with an untitled session without
// refs. It returns the ingested database.
func summaryFixture(t *testing.T) string {
	t.Helper()
	projects := filepath.Join(t.TempDir(), "projects")
	k, w := "/w/kompass", "/w/wetterwarte"
	k1, k2, k3, w1 := "11110000-0000-4000-8000-000000000001", "11110000-0000-4000-8000-000000000002", "11110000-0000-4000-8000-000000000003", "11110000-0000-4000-8000-000000000004"
	tf.WriteFile(t, projects, "-w-kompass/"+k1+".jsonl", tf.Join(
		tf.AITitle(k1, "SBOM"),
		tf.User(k1, "a1", "2026-11-03T08:00:00.000Z", k, "Add the SBOM, see #31 and PR #32."),
		tf.ToolUse(k1, "a2", "2026-11-03T08:00:10.000Z", k, "Bash", "gh issue list"),
		tf.ToolResult(k1, "a3", "2026-11-03T08:00:20.000Z", k, "a2", "#999 tool output must not count"),
		tf.Assistant(k1, "a4", "2026-11-03T08:00:30.000Z", k, "Done; #888 in a reply must not count either."),
		tf.User(k1, "a5", "2026-11-03T08:10:00.000Z", k, "thanks"),
	))
	tf.WriteFile(t, projects, "-w-kompass/"+k2+".jsonl", tf.Join(
		tf.CustomTitle(k2, "Konfig (#21)"),
		tf.User(k2, "b1", "2026-11-03T08:20:00.000Z", k, "Konfig anpassen wie in #21 besprochen."),
		tf.Assistant(k2, "b2", "2026-11-03T08:20:30.000Z", k, "Erledigt."),
	))
	tf.WriteFile(t, projects, "-w-kompass/"+k3+".jsonl", tf.Join(
		tf.User(k3, "c1", "2026-11-03T08:30:00.000Z", k, "Fix #19 and #26, then look at https://github.com/mara/kompass/issues/27"),
		tf.Assistant(k3, "c2", "2026-11-03T08:30:30.000Z", k, "Fixed."),
	))
	tf.WriteFile(t, projects, "-w-wetterwarte/"+w1+".jsonl", tf.Join(
		tf.User(w1, "d1", "2026-11-03T08:40:00.000Z", w, "  Frostwarnung bauen:\nunter zwei Grad warnen.  "),
		tf.Assistant(w1, "d2", "2026-11-03T08:40:30.000Z", w, "Gebaut."),
	))
	git := map[string]gitinfo.Info{
		k: {Dir: k, Toplevel: k, Root: k, RemoteURL: "https://example.test/o/kompass", Branch: "main"},
		w: {Dir: w, Toplevel: w, Root: w, RemoteURL: "https://example.test/o/wetterwarte", Branch: "main"},
	}
	_, db := fixturedb.Ingest(t, projects, git)
	return db
}

// TestStatsWorktimeSummaryValues checks the refs and the booking text of
// every cell, project and session in JSON: references come from titles
// and own messages only, deduplicated and sorted; the text names the
// sessions per project.
func TestStatsWorktimeSummaryValues(t *testing.T) {
	t.Parallel()
	db := summaryFixture(t)
	e := newTestEnv(t)
	e.now = activityNow
	args := []string{"--worktime", "--since", "2026-11-03", "--until", "2026-11-03", "--json"}
	out := decode[WorktimeJSON](t, runActivity(t, e, db, args...).stdout)
	if out.SummaryMaxChars != 200 {
		t.Errorf("summary_max_chars = %d, want 200", out.SummaryMaxChars)
	}
	c, ok := cellOf(out, "2026-11-03", "kompass")
	if !ok {
		t.Fatalf("no kompass cell: %+v", out.Days)
	}
	if want := []string{"#19", "#21", "#26", "#31", "PR #32", "mara/kompass#27"}; !reflect.DeepEqual(c.Refs, want) {
		t.Errorf("kompass refs = %v, want %v", c.Refs, want)
	}
	if want := "kompass: SBOM (#31/PR #32), Konfig (#21), #19/#26/mara/kompass#27"; c.Text != want {
		t.Errorf("kompass text = %q, want %q", c.Text, want)
	}
	if len(c.Projects) != 1 || !reflect.DeepEqual(c.Projects[0].Refs, c.Refs) {
		t.Errorf("kompass projects = %+v", c.Projects)
	}
	if len(c.Sessions) != 3 {
		t.Fatalf("kompass sessions = %+v", c.Sessions)
	}
	for i, want := range [][]string{{"#31", "PR #32"}, {"#21"}, {"#19", "#26", "mara/kompass#27"}} {
		if !reflect.DeepEqual(c.Sessions[i].Refs, want) {
			t.Errorf("session %d refs = %v, want %v", i, c.Sessions[i].Refs, want)
		}
	}
	c, ok = cellOf(out, "2026-11-03", "wetterwarte")
	if !ok || len(c.Refs) != 0 || c.Refs == nil || c.Text != "wetterwarte: Frostwarnung bauen:" {
		t.Errorf("wetterwarte = %+v (ok %v)", c, ok)
	}
	if len(c.Sessions) != 1 || c.Sessions[0].Refs == nil || len(c.Sessions[0].Refs) != 0 {
		t.Errorf("wetterwarte sessions = %+v", c.Sessions)
	}
	// The raw JSON has the keys with empty lists, never null.
	raw := runActivity(t, e, db, args...).stdout
	if strings.Contains(raw, `"refs": null`) || strings.Contains(raw, `"text": null`) || !strings.Contains(raw, `"refs": []`) {
		t.Errorf("empty refs are not [] or text is null:\n%s", raw)
	}

	// One category for both projects: the refs are the union, the text
	// lists the projects by name.
	appendConfig(t, e, "\n[[worktime.category]]\nname = \"kunde\"\nmatch = \"https://example.test/o/*\"\n")
	out = decode[WorktimeJSON](t, runActivity(t, e, db, args...).stdout)
	c, ok = cellOf(out, "2026-11-03", "kunde")
	if !ok || len(c.Projects) != 2 || len(c.Projects[1].Refs) != 0 {
		t.Fatalf("kunde = %+v (ok %v)", c, ok)
	}
	if want := "kompass: SBOM (#31/PR #32), Konfig (#21), #19/#26/mara/kompass#27; wetterwarte: Frostwarnung bauen:"; c.Text != want {
		t.Errorf("kunde text = %q, want %q", c.Text, want)
	}

	// summary_max_chars cuts the text; 0 lifts the limit.
	appendConfig(t, e, "\n[worktime]\nsummary_max_chars = 20\n")
	out = decode[WorktimeJSON](t, runActivity(t, e, db, args...).stdout)
	if c, _ := cellOf(out, "2026-11-03", "kunde"); out.SummaryMaxChars != 20 || c.Text != "kompass: SBOM (#31/…" {
		t.Errorf("cut text = %q (max %d)", c.Text, out.SummaryMaxChars)
	}
}

// TestStatsWorktimeSummaryText compares the text output of --summary,
// alone and with --sessions, with golden files.
func TestStatsWorktimeSummaryText(t *testing.T) {
	t.Parallel()
	db := summaryFixture(t)
	for _, tt := range []struct {
		name   string
		config string
		args   []string
	}{
		{"Summary", "", []string{"--summary"}},
		{"SessionsSummary", "", []string{"--sessions", "--summary"}},
		{"CategorySummary", "\n[[worktime.category]]\nname = \"kunde\"\nmatch = \"https://example.test/o/*\"\n", []string{"--summary"}},
		{"Empty", "", []string{"--summary", "--since", "2026-11-09", "--until", "2026-11-10"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.now = activityNow
			appendConfig(t, e, tt.config)
			args := append([]string{"--worktime"}, tt.args...)
			if tt.name != "Empty" {
				args = append(args, "--since", "2026-11-03", "--until", "2026-11-03")
			}
			r := runActivity(t, e, db, args...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			golden.RequireEqual(t, []byte(r.stdout))
		})
	}
}
