package star

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// fingerprint summarises the published views; equal fingerprints mean equal
// analytics content.
var fingerprint = []string{
	"SELECT count(*), count(*) FILTER (WHERE actor = 'human'), count(*) FILTER (WHERE actor = 'agent'), count(*) FILTER (WHERE kind = 'tool_use'), count(DISTINCT session_id), sum(tokens_in) FROM analytics_events_v1",
	"SELECT count(*), sum(events), sum(duration_s), sum(human_events), sum(tool_calls) FROM analytics_sessions_v1",
	"SELECT string_agg(d || '=' || n, ',' ORDER BY d) FROM (SELECT day_local AS d, sum(human_prompts + agent_events) AS n FROM analytics_daily_v1 GROUP BY day_local)",
	"SELECT sum(calls), count(*) FROM analytics_tool_month_v1",
	"SELECT sum(sessions), sum(subagent_sessions) FROM analytics_session_month_v1",
	"SELECT string_agg(project || '=' || n, ',' ORDER BY project) FROM (SELECT project, count(*) AS n FROM analytics_sessions_v1 GROUP BY project)",
	"SELECT count(*), sum(raw_seconds), sum(rounded_seconds) FROM analytics_worktime_day_v1",
	"SELECT string_agg(project || '=' || n, ',' ORDER BY project) FROM (SELECT project, sum(raw_seconds) AS n FROM analytics_worktime_v1 GROUP BY project)",
}

// snapshot evaluates the fingerprint queries.
func snapshot(t *testing.T, db *duck.DB) []string {
	t.Helper()
	out := make([]string, len(fingerprint))
	for i, q := range fingerprint {
		out[i] = row(t, db, q)
	}
	return out
}

func compareSnapshots(t *testing.T, what string, want, got []string) {
	t.Helper()
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("%s: %s\nwant %s\ngot  %s", what, fingerprint[i], want[i], got[i])
		}
	}
}

func TestRunIsIdempotent(t *testing.T) {
	archive := demoCopy(t)
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	first := mustRun(t, db, opts(t, archive))
	if first.Mode != "full" || first.SeqFrom != 0 || first.SeqTo == 0 {
		t.Fatalf("first run %+v", first)
	}
	before := snapshot(t, db)
	second := mustRun(t, db, opts(t, archive))
	if second.Mode != "incremental" || second.Events != 0 || second.SeqFrom != first.SeqTo {
		t.Errorf("second run %+v, want incremental with nothing staged", second)
	}
	o := opts(t, archive)
	o.Full = true
	third := mustRun(t, db, o)
	if third.Mode != "full" || third.Events != first.Events {
		t.Errorf("full rerun %+v, want %d events", third, first.Events)
	}
	compareSnapshots(t, "rerun", before, snapshot(t, db))
	if got := row(t, db, "SELECT count(*), max(batch_id) FROM etl_batch"); got != "3|3" {
		t.Errorf("batches %s, want 3|3", got)
	}
}

// TestIncrementalEqualsFull grows an archive between two runs: the
// incremental load must end where a full load of the grown archive ends.
func TestIncrementalEqualsFull(t *testing.T) {
	archive := demoCopy(t)
	// hold back the last tenth of the events and the sessions only they have
	execSQLite(t, archive, "CREATE TABLE held AS SELECT * FROM events WHERE seq > (SELECT max(seq) * 9 / 10 FROM events)",
		"DELETE FROM events WHERE seq IN (SELECT seq FROM held)")
	db := openDuck(t, filepath.Join(t.TempDir(), "inc.duckdb"))
	mustRun(t, db, opts(t, archive))
	execSQLite(t, archive, "INSERT INTO events SELECT * FROM held", "DROP TABLE held")
	r := mustRun(t, db, opts(t, archive))
	if r.Mode != "incremental" || r.Events == 0 {
		t.Fatalf("incremental run %+v", r)
	}
	full := openDuck(t, filepath.Join(t.TempDir(), "full.duckdb"))
	mustRun(t, full, opts(t, archive))
	compareSnapshots(t, "incremental vs full", snapshot(t, full), snapshot(t, db))
}

// TestDimensionsAccumulate: a member whose facts are gone keeps its key.
func TestDimensionsAccumulate(t *testing.T) {
	archive := demoCopy(t)
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	mustRun(t, db, opts(t, archive))
	tools := row(t, db, "SELECT count(*) FROM dim_tool")
	execSQLite(t, archive, "DELETE FROM events WHERE tool_name = 'Grep'")
	o := opts(t, archive)
	o.Full = true
	mustRun(t, db, o)
	if got := row(t, db, "SELECT count(*) FROM dim_tool"); got != tools {
		t.Errorf("dim_tool has %s rows after the reload, want %s", got, tools)
	}
	if got := row(t, db, "SELECT count(*) FROM fact_event f JOIN dim_tool d USING (tool_key) WHERE d.tool_name = 'Grep'"); got != "0" {
		t.Errorf("Grep facts after reload: %s", got)
	}
}

// onHostA splits the demo archive into two hosts: dotfiles, fahrtenbuch and
// the kompass worktree on host-a, the rest on host-b; subagents go with
// their parent.
const onHostA = `(SELECT s.id FROM sessions s LEFT JOIN sessions p ON p.id = s.parent_id
	WHERE COALESCE(p.cwd, s.cwd) LIKE '%/dotfiles%' OR COALESCE(p.cwd, s.cwd) LIKE '%/fahrtenbuch%'
	   OR COALESCE(p.cwd, s.cwd) LIKE '%/kompass\_\_worktrees/%' ESCAPE '\')`

// TestTwoSources loads two archives with their own watermarks into one
// database: together they equal one load of the whole archive, and a full
// reload of one replaces only its own facts.
func TestTwoSources(t *testing.T) {
	a, b := demoCopy(t), demoCopy(t)
	execSQLite(t, a, "DELETE FROM events WHERE session_id NOT IN "+onHostA, "DELETE FROM sessions WHERE id NOT IN "+onHostA)
	execSQLite(t, b, "DELETE FROM events WHERE session_id IN "+onHostA, "DELETE FROM sessions WHERE id IN "+onHostA)
	db := openDuck(t, filepath.Join(t.TempDir(), "pool.duckdb"))
	oa, ob := opts(t, a), opts(t, b)
	ob.Source, ob.Host = "host-b", "host-b"
	mustRun(t, db, oa)
	mustRun(t, db, ob)
	oa.Full = true
	mustRun(t, db, oa)

	whole := openDuck(t, filepath.Join(t.TempDir(), "whole.duckdb"))
	mustRun(t, whole, opts(t, os.Getenv(fixturedb.DemoEnv)))
	compareSnapshots(t, "pool vs whole", snapshot(t, whole), snapshot(t, db))

	got := row(t, db, "SELECT string_agg(host || '=' || n, ' ' ORDER BY host) FROM (SELECT host, count(*) AS n FROM analytics_sessions_v1 GROUP BY host)")
	if want := "host-a=" + sqliteScalar(t, a, "SELECT count(DISTINCT session_id) FROM events") + " host-b=" + sqliteScalar(t, b, "SELECT count(DISTINCT session_id) FROM events"); got != want {
		t.Errorf("sessions per host %s, want %s", got, want)
	}
	if w := row(t, db, "SELECT string_agg(source || ':' || last_seq, ' ' ORDER BY source) FROM etl_watermark"); w == "" {
		t.Error("no watermarks")
	}
	t.Logf("sessions per host: %s", got)
}

// sqliteScalar reads one value from an archive outside the ETL.
func sqliteScalar(t *testing.T, path, q string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var v string
	if err := db.QueryRow(q).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}
