package analytics

import (
	"context"
	"database/sql"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

var update = flag.Bool("update", false, "rewrite contract.txt from the current views")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(fixturedb.DemoMain(m))
}

// demoCopy copies the demo archive into the test's temp dir, so a test
// may change it.
func demoCopy(t *testing.T) string {
	t.Helper()
	src := os.Getenv(fixturedb.DemoEnv)
	dst := filepath.Join(t.TempDir(), "archive.db")
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dst
}

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func opts(t *testing.T, archive, target string) Options {
	t.Helper()
	return Options{Archive: archive, Target: target, Source: "host-a", Host: "host-a", TZ: berlin(t), Home: demo.Home}
}

func mustRun(t *testing.T, o Options) Report {
	t.Helper()
	r, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return r
}

// exec runs statements against a database file outside the ETL.
func exec(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// counts returns row counts of the given tables of the target.
func counts(t *testing.T, target string, tables ...string) map[string]int {
	t.Helper()
	db, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	out := map[string]int{}
	for _, tb := range tables {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + tb).Scan(&n); err != nil {
			t.Fatalf("%s: %v", tb, err)
		}
		out[tb] = n
	}
	return out
}

var allTables = []string{
	"fact_event", "fact_session", "dim_host", "dim_session", "dim_project", "dim_repo", "dim_model",
	"dim_tool", "dim_actor", "dim_event_type", "dim_account", "dim_harness", "mart_daily",
	"mart_project_week", "mart_tool_month", "mart_session_month", "mart_weekhour",
}

func TestRunDemo(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "analytics.db")
	o := opts(t, os.Getenv(fixturedb.DemoEnv), target)

	first := mustRun(t, o)
	if first.Mode != "full" || first.Events != 21942 || first.Sessions != 936 {
		t.Errorf("first run = %+v, want full with 21942 events in 936 sessions", first)
	}
	want := counts(t, target, allTables...)
	if want["fact_event"] != 21942 || want["fact_session"] != 936 {
		t.Errorf("facts = %d events, %d sessions", want["fact_event"], want["fact_session"])
	}

	t.Run("incremental without new events is a no-op", func(t *testing.T) {
		r := mustRun(t, o)
		if r.Mode != "incremental" || r.Events != 0 {
			t.Errorf("second run = %+v, want incremental with 0 events", r)
		}
		if got := counts(t, target, allTables...); !equalCounts(got, want) {
			t.Errorf("counts changed: %v, want %v", got, want)
		}
	})
	t.Run("full reload is idempotent", func(t *testing.T) {
		o := o
		o.Full = true
		mustRun(t, o)
		if got := counts(t, target, allTables...); !equalCounts(got, want) {
			t.Errorf("counts changed: %v, want %v", got, want)
		}
	})
}

// TestIncrementalEqualsFull loads a truncated copy of the archive, then the
// whole archive incrementally, and compares with one full load.
func TestIncrementalEqualsFull(t *testing.T) {
	t.Parallel()
	whole := os.Getenv(fixturedb.DemoEnv)
	part := demoCopy(t)
	exec(t, part, "DELETE FROM events WHERE seq > 12000")

	inc := filepath.Join(t.TempDir(), "inc.db")
	r1 := mustRun(t, opts(t, part, inc))
	r2 := mustRun(t, opts(t, whole, inc))
	if r1.SeqTo != 12000 || r2.SeqFrom != 12000 || r2.Mode != "incremental" {
		t.Errorf("batches %+v, %+v: want the second to start at seq 12000", r1, r2)
	}
	full := filepath.Join(t.TempDir(), "full.db")
	mustRun(t, opts(t, whole, full))

	if a, b := counts(t, inc, allTables...), counts(t, full, allTables...); !equalCounts(a, b) {
		t.Errorf("incremental %v != full %v", a, b)
	}
	for _, q := range []string{
		"SELECT SUM(human_prompts), SUM(agent_events), SUM(tool_calls), SUM(sessions) FROM analytics_daily_v1",
		"SELECT SUM(duration_s), SUM(events), SUM(model_switches) FROM analytics_sessions_v1",
		"SELECT SUM(calls) FROM analytics_tool_month_v1",
	} {
		if a, b := scalarRow(t, inc, q), scalarRow(t, full, q); a != b {
			t.Errorf("%s: incremental %s != full %s", q, a, b)
		}
	}
}

// TestDimensionsAccumulate: a later load from an archive that no longer has
// some members keeps them, so the keys frozen in older facts stay valid.
func TestDimensionsAccumulate(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "analytics.db")
	mustRun(t, opts(t, os.Getenv(fixturedb.DemoEnv), target))
	before := counts(t, target, "dim_project", "dim_tool", "dim_session", "dim_repo")

	small := demoCopy(t)
	exec(t, small, "DELETE FROM events WHERE session_id IN (SELECT id FROM sessions WHERE cwd LIKE '%/notizen%')")
	o := opts(t, small, target)
	o.Full = true
	mustRun(t, o)
	after := counts(t, target, "dim_project", "dim_tool", "dim_session", "dim_repo")
	if !equalCounts(before, after) {
		t.Errorf("dimensions shrank: %v -> %v", before, after)
	}
}

// TestHostFromSource: an archive with sessions.host (the central archive of
// #347) names the host per session; the option is only the fallback.
func TestHostFromSource(t *testing.T) {
	t.Parallel()
	src := demoCopy(t)
	exec(t, src,
		"ALTER TABLE sessions ADD COLUMN host TEXT",
		"UPDATE sessions SET host = CASE WHEN cwd LIKE '%/kompass%' OR cwd LIKE '%/wetterwarte%' THEN 'host-b' END")
	target := filepath.Join(t.TempDir(), "analytics.db")
	mustRun(t, opts(t, src, target))
	got := scalarRow(t, target, "SELECT group_concat(host || '=' || n, ' ') FROM (SELECT host, COUNT(*) AS n FROM analytics_sessions_v1 GROUP BY host ORDER BY host)")
	want := scalarRow(t, src, "SELECT 'host-a=' || SUM(COALESCE(host, '') = '') || ' host-b=' || SUM(host = 'host-b') FROM sessions WHERE id IN (SELECT session_id FROM events)")
	if got != want {
		t.Errorf("sessions per host = %s, want %s", got, want)
	}
}

// TestContract compares the columns of the published views with
// contract.txt (go test ./internal/analytics -run TestContract -update).
func TestContract(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "analytics.db")
	mustRun(t, opts(t, os.Getenv(fixturedb.DemoEnv), target))
	db, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	got, err := describeViews(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("contract.txt", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got != contract {
		t.Errorf("views differ from contract.txt; got:\n%s", got)
	}
}

func scalarRow(t *testing.T, path, q string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if !rows.Next() {
		t.Fatalf("%s: no row", q)
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	s := ""
	for i, v := range vals {
		if i > 0 {
			s += "|"
		}
		s += v.String
	}
	return s
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
