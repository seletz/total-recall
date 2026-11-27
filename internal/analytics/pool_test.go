package analytics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// onHostA is the split of the demo archive into two hosts, as in the pool
// demo of #347: dotfiles, fahrtenbuch and the kompass worktree on host-a,
// the rest on host-b. Subagents go with their parent's directory.
const onHostA = `(SELECT s.id FROM sessions s LEFT JOIN sessions p ON p.id = s.parent_id
	WHERE COALESCE(p.cwd, s.cwd) LIKE '%/dotfiles%' OR COALESCE(p.cwd, s.cwd) LIKE '%/fahrtenbuch%'
	   OR COALESCE(p.cwd, s.cwd) LIKE '%/kompass\_\_worktrees/%' ESCAPE '\')`

// splitDemo writes the host-a and host-b archives of the demo pool.
func splitDemo(t *testing.T) (a, b string) {
	t.Helper()
	a, b = demoCopy(t), demoCopy(t)
	exec(t, a, "DELETE FROM events WHERE session_id NOT IN "+onHostA, "DELETE FROM sessions WHERE id NOT IN "+onHostA)
	exec(t, b, "DELETE FROM events WHERE session_id IN "+onHostA, "DELETE FROM sessions WHERE id IN "+onHostA)
	return a, b
}

// TestPoolOfArchives loads the archives of two hosts, each with its own
// watermark, into one target. The pool must equal one load of the whole
// archive (sum equals parts over hosts), with the host as a dimension.
func TestPoolOfArchives(t *testing.T) {
	t.Parallel()
	a, b := splitDemo(t)
	pool := filepath.Join(t.TempDir(), "pool.db")
	oa, ob := opts(t, a, pool), opts(t, b, pool)
	ob.Source, ob.Host = "host-b", "host-b"
	mustRun(t, oa)
	mustRun(t, ob)
	// a full reload of one host replaces only that host's facts
	oa.Full = true
	mustRun(t, oa)

	whole := filepath.Join(t.TempDir(), "whole.db")
	mustRun(t, opts(t, os.Getenv(fixturedb.DemoEnv), whole))

	for _, q := range []string{
		"SELECT COUNT(*), SUM(actor = 'human'), SUM(actor = 'agent'), SUM(kind = 'tool_use'), COUNT(DISTINCT session_id) FROM analytics_events_v1",
		"SELECT SUM(events), SUM(duration_s), COUNT(*) FROM analytics_sessions_v1",
		"SELECT group_concat(d, ','), SUM(n) FROM (SELECT day_local AS d, SUM(human_prompts + agent_events) AS n FROM analytics_daily_v1 GROUP BY day_local ORDER BY day_local)",
		"SELECT SUM(calls) FROM analytics_tool_month_v1",
	} {
		if p, w := scalarRow(t, pool, q), scalarRow(t, whole, q); p != w {
			t.Errorf("%s:\npool  %s\nwhole %s", q, p, w)
		}
	}
	perHost := scalarRow(t, pool, "SELECT group_concat(host || '=' || n, ' ') FROM (SELECT host, COUNT(*) AS n FROM analytics_sessions_v1 GROUP BY host ORDER BY host)")
	want := "host-a=" + scalarRow(t, a, "SELECT COUNT(DISTINCT session_id) FROM events") + " host-b=" + scalarRow(t, b, "SELECT COUNT(DISTINCT session_id) FROM events")
	if perHost != want {
		t.Errorf("sessions per host %s, want %s", perHost, want)
	}
	t.Logf("pool: %s", perHost)
}
