package cli

import (
	"os"
	"strings"
	"testing"

	tf "github.com/seletz/total-recall/internal/testfixture"
)

const reindexSession = "7d1e02c0-0000-4000-8000-00000000ab11"

// writeCoordinatorDay adds a day with a typed message in the morning and a
// task block pasted by a coordinator late in the evening (2026-10-07, CEST:
// 10:00 and 22:30 local).
func writeCoordinatorDay(t *testing.T, e *testEnv) {
	t.Helper()
	paste := "<pasted_content id=\"4a2b\">\n# Task: rebase the branch\nThen open the PR.\n</pasted_content id=\"4a2b\">\n"
	tf.WriteFile(t, e.projects, "-w-alpha/"+reindexSession+".jsonl", tf.Join(
		tf.User(reindexSession, "p1", "2026-10-07T08:00:00.000Z", "/w/alpha", "please list the files"),
		tf.Assistant(reindexSession, "p2", "2026-10-07T08:00:02.000Z", "/w/alpha", "Done."),
		tf.User(reindexSession, "p3", "2026-10-07T20:30:00.000Z", "/w/alpha", paste),
		tf.Assistant(reindexSession, "p4", "2026-10-07T20:30:05.000Z", "/w/alpha", "Rebased."),
	))
}

func (e *testEnv) appendConfig(t *testing.T, s string) {
	t.Helper()
	f, err := os.OpenFile(e.configPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

type workdayDay struct {
	Day       string `json:"day"`
	LastLocal string `json:"last_local"`
	Span      int    `json:"span_seconds"`
	Human     int    `json:"human"`
}

type worktimeDay struct {
	Day string `json:"day"`
	Raw int    `json:"raw_seconds"`
}

// TestReindexActors runs the whole story of issue 111: a pasted task is
// ingested as human and stretches the day; after the paste-header rule is
// configured, reindex --actors turns it into a peer message and --workday
// and --worktime no longer count it.
func TestReindexActors(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	writeCoordinatorDay(t, e)
	e.mustRun(t, "ingest", "--all")
	day := func() workdayDay {
		r := decode[struct {
			Days []workdayDay `json:"days"`
		}](t, e.mustRun(t, "stats", "--workday", "--since", "2026-10-07", "--until", "2026-10-07", "--json"))
		if len(r.Days) != 1 || r.Days[0].Day != "2026-10-07" {
			t.Fatalf("days = %+v", r.Days)
		}
		return r.Days[0]
	}
	worktime := func() worktimeDay {
		r := decode[struct {
			Days []worktimeDay `json:"days"`
		}](t, e.mustRun(t, "stats", "--worktime", "--since", "2026-10-07", "--until", "2026-10-07", "--json"))
		if len(r.Days) != 1 || r.Days[0].Day != "2026-10-07" {
			t.Fatalf("days = %+v", r.Days)
		}
		return r.Days[0]
	}
	if d := day(); d.LastLocal != "22:30" || d.Human != 2 {
		t.Fatalf("before: workday = %+v, want last 22:30 with 2 human events", d)
	}
	if w := worktime(); w.Raw != 360 {
		t.Fatalf("before: worktime raw = %d, want 360 (two credits of 3m)", w.Raw)
	}

	// Same rules: nothing to do.
	out := e.mustRun(t, "reindex", "--actors")
	if !strings.Contains(out, "0 changed") {
		t.Errorf("reindex with unchanged rules: %q", out)
	}

	e.appendConfig(t, "\n[harness.claude.actors]\npeer_paste_headers = [\"# Task:\"]\n")
	out = e.mustRun(t, "reindex", "--actors", "--dry-run")
	if !strings.Contains(out, "dry run") || !strings.Contains(out, "1 would change") || !strings.Contains(out, "human -> peer: 1") {
		t.Errorf("dry run output = %q", out)
	}
	if n := e.count(t, "SELECT count(*) FROM events WHERE actor = 'peer' AND session_id = 'claude:"+reindexSession+"'"); n != 0 {
		t.Errorf("dry run changed %d events", n)
	}

	out = e.mustRun(t, "reindex", "--actors")
	if !strings.Contains(out, "1 changed") || !strings.Contains(out, "human -> peer: 1") {
		t.Errorf("reindex output = %q", out)
	}
	if d := day(); d.LastLocal != "10:00" || d.Span != 0 || d.Human != 1 {
		t.Errorf("after: workday = %+v, want last 10:00 with 1 human event", d)
	}
	if w := worktime(); w.Raw != 180 {
		t.Errorf("after: worktime raw = %d, want 180", w.Raw)
	}
	if out := e.mustRun(t, "reindex", "--actors"); !strings.Contains(out, "0 changed") {
		t.Errorf("second run not idempotent: %q", out)
	}
	// A freshly ingested paste follows the rule directly.
	tf.AppendFile(t, e.projects+"/-w-alpha/"+reindexSession+".jsonl",
		tf.User(reindexSession, "p5", "2026-10-07T21:00:00.000Z", "/w/alpha", "<pasted_content id=\"9\">\n# Task: next\n</pasted_content id=\"9\">\n").JSON())
	e.mustRun(t, "ingest", "--all")
	if n := e.count(t, "SELECT count(*) FROM events WHERE actor = 'peer' AND session_id = 'claude:"+reindexSession+"'"); n != 2 {
		t.Errorf("peer events after ingest = %d, want 2", n)
	}
}

func TestReindexUsage(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	if r := e.run(t, "", "reindex"); r.code == 0 || !strings.Contains(r.stderr, "--actors") {
		t.Errorf("reindex without flags = %+v", r)
	}
	if r := e.run(t, "", "reindex", "--dry-run"); r.code == 0 || !strings.Contains(r.stderr, "--actors") {
		t.Errorf("reindex --dry-run alone = %+v", r)
	}
}
