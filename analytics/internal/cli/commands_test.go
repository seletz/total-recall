package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	trcli "github.com/seletz/total-recall/internal/cli"
	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

func TestMain(m *testing.M) { os.Exit(fixturedb.DemoMain(m)) }

// withTokens gives the demo copy token counts and three models (the demo
// archive has none yet, #365).
const withTokens = `UPDATE events SET
	tokens_in = 1000 + seq % 997,
	tokens_out = 50 + seq % 331,
	tokens_cache_read = CASE WHEN seq % 5 = 0 THEN NULL ELSE 400 + seq % 401 END,
	tokens_cache_write = CASE WHEN seq % 5 = 0 THEN NULL ELSE seq % 97 END,
	model = CASE seq % 3 WHEN 0 THEN 'kompass-model-opus-x' WHEN 1 THEN 'kompass-model-sonnet-y' ELSE 'kompass-model-haiku-z' END
 WHERE role = 'assistant' AND kind = 'message'`

// env is a demo archive with a config pointing at it and an analytics
// database path; run executes the analytics CLI in it.
type env struct {
	t         *testing.T
	archive   string
	cfg       string
	analytics string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.db")
	in, err := os.Open(os.Getenv(fixturedb.DemoEnv))
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	_ = in.Close()
	_ = out.Close()
	db, err := sql.Open("sqlite", archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(withTokens); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, "db = %q\ntimezone = \"Europe/Berlin\"\n\n[git]\nresolve = false\n", archive), 0o644); err != nil {
		t.Fatal(err)
	}
	return &env{t: t, archive: archive, cfg: cfg, analytics: filepath.Join(dir, "analytics.duckdb")}
}

func (e *env) getenv(k string) string {
	if k == "TOTAL_RECALL_CONFIG" {
		return e.cfg
	}
	return ""
}

func (e *env) run(args ...string) (int, string, string) {
	e.t.Helper()
	var out, errb bytes.Buffer
	code := Main(&App{
		Args: args, Stdout: &out, Stderr: &errb, Getenv: e.getenv, Home: demo.Home,
		Now:      func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) },
		Hostname: func() (string, error) { return "host-a.example", nil },
	})
	return code, out.String(), errb.String()
}

func (e *env) mustRun(args ...string) string {
	e.t.Helper()
	code, out, errOut := e.run(args...)
	if code != 0 {
		e.t.Fatalf("%v: exit %d: %s%s", args, code, errOut, out)
	}
	return out
}

func (e *env) json(v any, args ...string) {
	e.t.Helper()
	out := e.mustRun(append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		e.t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// stats runs `total-recall stats --json` on the same archive and config.
func (e *env) stats(args ...string) map[string]any {
	e.t.Helper()
	var out, errb bytes.Buffer
	code := trcli.Main(&trcli.App{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Home: demo.Home, Getenv: e.getenv,
		Args: append([]string{"stats", "--since", "2026-05-01", "--until", "2026-11-30", "--json"}, args...),
		Now:  func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) },
	})
	if code != 0 {
		e.t.Fatalf("stats %v: exit %d: %s", args, code, errb.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// keys checks that every key is present in each object of a JSON document.
func keys(t *testing.T, doc string, want ...string) {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatal(err)
	}
	objs, ok := v.([]any)
	if !ok {
		objs = []any{v}
	}
	for _, o := range objs {
		m := o.(map[string]any)
		for _, k := range want {
			if _, ok := m[k]; !ok {
				t.Errorf("key %q missing in %v", k, m)
			}
		}
	}
}

func TestBuildStatusCheck(t *testing.T) {
	e := newEnv(t)
	var b BuildJSON
	e.json(&b, "build", "--db", e.analytics)
	if b.Mode != "full" || b.Host != "host-a" || b.Source != "host-a" || b.Events != 21942 || b.Sessions != 936 || b.Tests == 0 {
		t.Errorf("first build %+v", b)
	}
	keys(t, e.mustRun("build", "--db", e.analytics, "--json"), "analytics_db", "source", "host", "archive", "tz", "batch_id", "mode",
		"seq_from", "seq_to", "events", "sessions", "models", "tests", "elapsed_ms")
	if out := e.mustRun("build", "--db", e.analytics); !strings.Contains(out, "(incremental)") || !strings.Contains(out, ": 0 events in 0 sessions") {
		t.Errorf("third build: %s", out)
	}

	var s StatusJSON
	e.json(&s, "status", "--db", e.analytics)
	if len(s.Sources) != 1 || s.Sources[0].Events != 21942 || s.Sources[0].Sessions != 936 || s.Sources[0].Backlog != 0 || !s.Sources[0].ArchiveFound ||
		s.TZ != "Europe/Berlin" || s.Views != 11 || s.Sources[0].LastBatch.BatchID != 3 {
		t.Errorf("status %+v", s)
	}
	// new events show up as backlog
	db, _ := sql.Open("sqlite", e.archive)
	if _, err := db.Exec("DELETE FROM events WHERE seq > (SELECT max(seq) - 10 FROM events)"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	e.mustRun("build", "--db", e.analytics, "--full")
	db, _ = sql.Open("sqlite", e.archive)
	if _, err := db.Exec("INSERT INTO events (id, session_id, ts, kind, role, actor) SELECT 'new-' || seq, session_id, ts, kind, role, actor FROM events ORDER BY seq DESC LIMIT 3"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	e.json(&s, "status", "--db", e.analytics)
	if s.Sources[0].Backlog != 3 {
		t.Errorf("backlog %d, want 3", s.Sources[0].Backlog)
	}
	keys(t, e.mustRun("status", "--db", e.analytics, "--json"), "analytics_db", "size_bytes", "tz", "zones", "views", "sources")

	var c CheckJSON
	e.json(&c, "check", "--db", e.analytics)
	if !c.OK || !c.ContractOK || len(c.Tests) == 0 || c.ContractDiff == nil {
		t.Errorf("check %+v", c)
	}
	if out := e.mustRun("check", "--db", e.analytics); !strings.HasPrefix(out, "contract ") && strings.Contains(strings.SplitN(out, "\n", 2)[0], " ok") {
		t.Errorf("check text: %s", out)
	}
}

func TestCommandsBeforeBuild(t *testing.T) {
	e := newEnv(t)
	for _, cmd := range []string{"status", "check", "usage", "tokens"} {
		code, out, _ := e.run(cmd, "--db", e.analytics, "--json")
		if code != 1 || !strings.Contains(out, `"kind":"db"`) || !strings.Contains(out, "run build first") {
			t.Errorf("%s before build: exit %d, %s", cmd, code, out)
		}
	}
}

// TestTUIErrors: tui says what is missing before it takes the terminal.
func TestTUIErrors(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"tui", "--db", e.analytics}, "run build first"},
		{[]string{"tui", "--db", e.analytics, "--show", "nope"}, "--show nope: want events, human, agent, sessions, tool calls, tokens or hours"},
	} {
		code, out, errOut := e.run(tc.args...)
		if code == 0 || !strings.Contains(out+errOut, tc.want) {
			t.Errorf("%v: exit %d, %s%s", tc.args, code, out, errOut)
		}
	}
}

func TestViews(t *testing.T) {
	e := newEnv(t)
	var vs []ViewJSON
	e.json(&vs, "views")
	if len(vs) != 11 {
		t.Fatalf("views %d, want 11", len(vs))
	}
	e.json(&vs, "views", "analytics_daily_v1")
	if len(vs) != 1 || vs[0].Columns[0].Name != "day_local" || vs[0].Columns[0].Type != "DATE" {
		t.Errorf("daily_v1 %+v", vs)
	}
	if code, out, _ := e.run("views", "nope", "--json"); code != 1 || !strings.Contains(out, `"kind":"usage"`) {
		t.Errorf("unknown view: %d %s", code, out)
	}
}

func TestQuery(t *testing.T) {
	e := newEnv(t)
	e.mustRun("build", "--db", e.analytics)
	q := "SELECT host, count(*) AS sessions, min(started_at) AS first, max(day) AS last_day FROM analytics_sessions_v1, (SELECT max(day_local) AS day FROM analytics_daily_v1) GROUP BY host"
	for _, tc := range []struct{ format, want string }{
		{"json", `"host": "host-a"`},
		{"jsonl", `{"host":"host-a","sessions":936,"first":"2026-05-04T07:02:00Z","last_day":"2026-10-31"}`},
		{"csv", "host,sessions,first,last_day\nhost-a,936,"},
		{"table", "host    sessions"},
	} {
		out := e.mustRun("query", "--db", e.analytics, "--format", tc.format, q)
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s: %q lacks %q", tc.format, out, tc.want)
		}
	}
	for _, bad := range []string{
		"CREATE TABLE x AS SELECT 1",
		"SELECT * FROM read_csv('" + e.cfg + "')",
		"ATTACH '" + e.archive + "' AS a (TYPE sqlite)",
		"SET enable_external_access = true",
		"SELECT nope",
	} {
		code, out, _ := e.run("query", "--db", e.analytics, "--json", bad)
		if code != 1 || !strings.Contains(out, `"kind":"`) {
			t.Errorf("%s: exit %d, %s", bad, code, out)
		}
	}
}

func TestExport(t *testing.T) {
	e := newEnv(t)
	e.mustRun("build", "--db", e.analytics)
	dir := t.TempDir()
	var ex []ExportJSON
	e.json(&ex, "export", "--db", e.analytics, "--out", dir)
	if len(ex) != 11 {
		t.Fatalf("exported %d views", len(ex))
	}
	for _, x := range ex {
		if fi, err := os.Stat(x.Path); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", x.Path, err)
		}
	}
	e.json(&ex, "export", "--db", e.analytics, "--out", dir, "--format", "csv", "analytics_sessions_v1")
	b, err := os.ReadFile(filepath.Join(dir, "analytics_sessions_v1.csv"))
	if err != nil || len(ex) != 1 || ex[0].Rows != 936 || strings.Count(string(b), "\n") != 937 {
		t.Errorf("csv export: %v, %+v", err, ex)
	}
}

func num(v any) int64 { return int64(v.(float64)) }

// TestUsageAndTokensSumEqualsParts compares the reports with stats --json
// on the same archive.
func TestUsageAndTokensSumEqualsParts(t *testing.T) {
	e := newEnv(t)
	e.mustRun("build", "--db", e.analytics)
	st := e.stats()

	var total []UsageJSON
	e.json(&total, "usage", "--db", e.analytics, "--by", "none")
	tt := st["total"].(map[string]any)
	if len(total) != 1 || total[0].Sessions != num(tt["sessions"]) || total[0].Human != num(tt["human"]) || total[0].Agent != num(tt["agent"]) {
		t.Errorf("usage total %+v, stats %v", total, tt)
	}
	var days []UsageJSON
	e.json(&days, "usage", "--db", e.analytics, "--by", "none", "--period", "day")
	byDay := map[string]UsageJSON{}
	for _, d := range days {
		byDay[d.Period] = d
	}
	n := 0
	for _, it := range st["by_day"].([]any) {
		o := it.(map[string]any)
		if num(o["human"])+num(o["agent"]) == 0 {
			continue
		}
		n++
		d := byDay[o["day"].(string)]
		if d.Sessions != num(o["sessions"]) || d.Human != num(o["human"]) || d.Agent != num(o["agent"]) {
			t.Errorf("day %s: usage %+v, stats %v", o["day"], d, o)
		}
	}
	if n != len(days) {
		t.Errorf("usage has %d days, stats %d", len(days), n)
	}
	// parts add up to the total in every grouping
	for _, by := range []string{"project", "host", "model", "tool", "repo"} {
		var rows []UsageJSON
		e.json(&rows, "usage", "--db", e.analytics, "--by", by)
		var h, a, tc, ti int64
		for _, r := range rows {
			h, a, tc, ti = h+r.Human, a+r.Agent, tc+r.ToolCalls, ti+r.TokensIn
		}
		if h != total[0].Human || a != total[0].Agent || tc != total[0].ToolCalls || ti != total[0].TokensIn {
			t.Errorf("--by %s: parts %d/%d/%d/%d, total %+v", by, h, a, tc, ti, total[0])
		}
	}

	tok := st["tokens"].(map[string]any)
	var models []TokensJSON
	e.json(&models, "tokens", "--db", e.analytics, "--by", "model")
	got := map[string]TokensJSON{}
	for _, m := range models {
		got[keyText(m.Key)] = m
	}
	for _, it := range tok["by_model"].([]any) {
		o := it.(map[string]any)
		m := got[o["model"].(string)]
		if m.Events != num(o["events"]) || m.Input != num(o["input"]) || m.Fresh != num(o["fresh"]) || m.CacheRead != num(o["cache_read"]) ||
			m.CacheWrite != num(o["cache_write"]) || m.Unsplit != num(o["unsplit"]) || m.Output != num(o["output"]) {
			t.Errorf("model %s: tokens %+v, stats %v", o["model"], m, o)
		}
	}
	if len(models) != len(tok["by_model"].([]any)) {
		t.Errorf("tokens has %d models, stats %d", len(models), len(tok["by_model"].([]any)))
	}
	keys(t, e.mustRun("usage", "--db", e.analytics, "--json"), "period", "key", "sessions", "human", "agent", "tool_calls", "tokens_in", "tokens_out")
	keys(t, e.mustRun("tokens", "--db", e.analytics, "--json"), "period", "key", "events", "input", "fresh", "cache_read", "cache_write", "unsplit", "output")
}

func TestReportFlags(t *testing.T) {
	e := newEnv(t)
	e.mustRun("build", "--db", e.analytics)
	for _, args := range [][]string{{"--by", "x"}, {"--period", "year"}, {"--since", "May"}} {
		code, out, _ := e.run(append([]string{"usage", "--db", e.analytics, "--json"}, args...)...)
		if code != 1 || !strings.Contains(out, `"kind":"usage"`) {
			t.Errorf("%v: exit %d, %s", args, code, out)
		}
	}
	var weeks []UsageJSON
	e.json(&weeks, "usage", "--db", e.analytics, "--period", "week", "--since", "2026-10-05", "--until", "2026-10-18")
	for _, w := range weeks {
		if w.Period != "2026-W41" && w.Period != "2026-W42" {
			t.Errorf("period %s outside the range", w.Period)
		}
	}
	if len(weeks) == 0 {
		t.Error("no weeks")
	}
}

// TestWorktimeSumEqualsParts: the worktime report equals
// stats --worktime --json in total, and its groupings add up.
func TestWorktimeSumEqualsParts(t *testing.T) {
	e := newEnv(t)
	e.mustRun("build", "--db", e.analytics)
	st := e.stats("--worktime")
	tot := st["total"].(map[string]any)
	var all []WorktimeJSON
	e.json(&all, "worktime", "--db", e.analytics, "--by", "none")
	if len(all) != 1 || all[0].RawSeconds != num(tot["raw_seconds"]) || all[0].RoundedSeconds != num(tot["rounded_seconds"]) || all[0].Days != num(tot["days"]) {
		t.Errorf("worktime total %+v, stats %v", all, tot)
	}
	for _, by := range []string{"category", "project", "host"} {
		var rows []WorktimeJSON
		e.json(&rows, "worktime", "--db", e.analytics, "--by", by)
		var raw, rounded int64
		for _, r := range rows {
			raw, rounded = raw+r.RawSeconds, rounded+r.RoundedSeconds
		}
		if raw != all[0].RawSeconds || (by == "category" && rounded != all[0].RoundedSeconds) {
			t.Errorf("--by %s: %d/%d, total %+v", by, raw, rounded, all[0])
		}
	}
	var days []WorktimeJSON
	e.json(&days, "worktime", "--db", e.analytics, "--by", "none", "--period", "day")
	got := map[string]WorktimeJSON{}
	for _, d := range days {
		got[d.Period] = d
	}
	for _, it := range st["days"].([]any) {
		d := it.(map[string]any)
		if num(d["raw_seconds"]) == 0 {
			continue
		}
		g := got[d["day"].(string)]
		if g.RawSeconds != num(d["raw_seconds"]) || g.RoundedSeconds != num(d["rounded_seconds"]) {
			t.Errorf("day %s: %+v, stats %v/%v", d["day"], g, d["raw_seconds"], d["rounded_seconds"])
		}
	}
	keys(t, e.mustRun("worktime", "--db", e.analytics, "--json"), "period", "key", "raw_seconds", "rounded_seconds", "days")
}
