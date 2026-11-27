package analytics

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/cli"
	"github.com/seletz/total-recall/internal/demo"
)

// withTokens gives the demo copy token counts and three models: the demo
// archive has neither. Deterministic from seq; every fifth event has no
// cache split (unsplit), so all four input parts occur.
var withTokens = []string{
	`UPDATE events SET
		tokens_in = 1000 + seq % 997,
		tokens_out = 50 + seq % 331,
		tokens_cache_read = CASE WHEN seq % 5 = 0 THEN NULL ELSE 400 + seq % 401 END,
		tokens_cache_write = CASE WHEN seq % 5 = 0 THEN NULL ELSE seq % 97 END,
		model = CASE seq % 3 WHEN 0 THEN 'kompass-model-opus-x' WHEN 1 THEN 'kompass-model-sonnet-y' ELSE 'kompass-model-haiku-z' END
	 WHERE role = 'assistant' AND kind = 'message'`,
}

// statsJSON runs the CLI with the demo home and Europe/Berlin.
func statsJSON(t *testing.T, archive string, args ...string) map[string]any {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(fmt.Sprintf("db = %q\ntimezone = \"Europe/Berlin\"\n\n[git]\nresolve = false\n", archive)), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	app := &cli.App{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
		Args: append([]string{"--db", archive, "stats", "--since", "2026-05-01", "--until", "2026-11-30", "--json"}, args...),
		Home: demo.Home,
		Getenv: func(k string) string {
			if k == "TOTAL_RECALL_CONFIG" {
				return cfg
			}
			return ""
		},
		Now: func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) },
	}
	if code := cli.Main(app); code != 0 {
		t.Fatalf("stats %v: exit %d: %s", args, code, errb.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("stats %v: %v\n%s", args, err, out.String())
	}
	return m
}

// rows returns a query's rows as "a|b|c" strings keyed by the first column.
func rowsByKey(t *testing.T, path, q string) map[string]string {
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
	out := map[string]string{}
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(cols)-1)
		for i := range parts {
			parts[i] = vals[i+1].String
		}
		out[vals[0].String] = strings.Join(parts, "|")
	}
	return out
}

func num(v any) string { return fmt.Sprintf("%.0f", v.(float64)) }

// fromJSON maps a list of JSON objects to "a|b|c" strings keyed by a field.
func fromJSON(t *testing.T, list any, key string, fields ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, it := range list.([]any) {
		o := it.(map[string]any)
		parts := make([]string, len(fields))
		for i, f := range fields {
			parts[i] = num(o[f])
		}
		k := fmt.Sprint(o[key])
		if f, ok := o[key].(float64); ok {
			k = num(f)
		}
		out[k] = strings.Join(parts, "|")
	}
	return out
}

// TestSumEqualsParts compares the published views with the --json output of
// stats on the same archive: totals, per day, per repo, per weekday and
// hour, tokens in total, per day and per model, tool calls per tool and per
// project. The archive is the demo with synthetic token counts.
func TestSumEqualsParts(t *testing.T) {
	t.Parallel()
	archive := demoCopy(t)
	exec(t, archive, withTokens...)
	target := filepath.Join(t.TempDir(), "analytics.db")
	mustRun(t, opts(t, archive, target))

	st := statsJSON(t, archive)
	ev := "FROM analytics_events_v1 WHERE actor IN ('human', 'agent')"
	act := "COUNT(DISTINCT session_id), SUM(actor = 'human'), SUM(actor = 'agent')"

	total := st["total"].(map[string]any)
	if got, want := scalarRow(t, target, "SELECT "+act+" "+ev), num(total["sessions"])+"|"+num(total["human"])+"|"+num(total["agent"]); got != want {
		t.Errorf("total: views %s, stats %s", got, want)
	}
	compare(t, "by_day",
		rowsByKey(t, target, "SELECT day_local, "+act+" "+ev+" GROUP BY day_local"),
		fromJSON(t, st["by_day"], "day", "sessions", "human", "agent"))
	compare(t, "by_day (daily_v1, events)",
		rowsByKey(t, target, "SELECT day_local, SUM(human_prompts), SUM(agent_events) FROM analytics_daily_v1 GROUP BY day_local"),
		fromJSON(t, st["by_day"], "day", "human", "agent"))
	compare(t, "by_repo",
		rowsByKey(t, target, "SELECT repo, "+act+" "+ev+" GROUP BY repo"),
		fromJSON(t, st["by_repo"], "repo", "sessions", "human", "agent"))
	wh := map[string]string{}
	for _, it := range st["by_weekday_hour"].([]any) {
		o := it.(map[string]any)
		if o["human"].(float64)+o["agent"].(float64) == 0 {
			continue
		}
		wh[fmt.Sprintf("%d-%s", int(o["weekday"].(float64))+1, num(o["hour"]))] = num(o["sessions"]) + "|" + num(o["human"]) + "|" + num(o["agent"])
	}
	compare(t, "by_weekday_hour",
		rowsByKey(t, target, "SELECT weekday_local || '-' || hour_local, "+act+" "+ev+" GROUP BY weekday_local, hour_local"), wh)
	compare(t, "by_weekday_hour (weekhour_v1, events)",
		rowsByKey(t, target, "SELECT iso_weekday || '-' || local_hour, human_events, agent_events FROM analytics_weekhour_v1"),
		func() map[string]string {
			out := map[string]string{}
			for k, v := range wh {
				out[k] = v[strings.Index(v, "|")+1:]
			}
			return out
		}())

	tok := st["tokens"].(map[string]any)
	tokFields := []string{"events", "input", "fresh", "cache_read", "cache_write", "unsplit", "output"}
	tokCols := "SUM(events), SUM(tokens_in), SUM(tokens_fresh), SUM(tokens_cache_read), SUM(tokens_cache_write), SUM(tokens_unsplit), SUM(tokens_out)"
	if num(tok["input"]) == "0" {
		t.Fatal("token injection did not reach stats")
	}
	compare(t, "tokens total",
		rowsByKey(t, target, "SELECT 'total', "+tokCols+" FROM analytics_tokens_day_v1"),
		func() keyed {
			parts := make([]string, len(tokFields))
			for i, f := range tokFields {
				parts[i] = num(tok[f])
			}
			return keyed{"total": strings.Join(parts, "|")}
		}())
	compare(t, "tokens by_day",
		rowsByKey(t, target, "SELECT day_local, "+tokCols+" FROM analytics_tokens_day_v1 GROUP BY day_local"),
		fromJSON(t, tok["by_day"], "day", tokFields...))
	compare(t, "tokens by_model",
		rowsByKey(t, target, "SELECT model, "+tokCols+" FROM analytics_tokens_day_v1 GROUP BY model"),
		fromJSON(t, tok["by_model"], "model", tokFields...))

	tools := statsJSON(t, archive, "--tools", "--by", "project")
	compare(t, "tools total",
		rowsByKey(t, target, "SELECT 'total', SUM(calls) FROM analytics_tool_month_v1"),
		map[string]string{"total": num(tools["total"])})
	compare(t, "tools by_tool",
		rowsByKey(t, target, "SELECT tool_name, SUM(calls) FROM analytics_tool_month_v1 GROUP BY tool_name"),
		fromJSON(t, tools["by_tool"], "tool", "count"))
	compare(t, "tools by project",
		rowsByKey(t, target, "SELECT project, SUM(calls) FROM analytics_tool_month_v1 GROUP BY project"),
		// --json keeps a directory project as its full path; the analytics
		// store abbreviates home to "~" (no user name, one project across
		// hosts with different home directories).
		func() keyed {
			out := keyed{}
			for k, v := range fromJSON(t, tools["groups"], "key", "count") {
				out[Tilde(k, demo.Home)] = v
			}
			return out
		}())
}

type keyed = map[string]string

func compare(t *testing.T, what string, views, stats keyed) {
	t.Helper()
	if len(stats) == 0 {
		t.Errorf("%s: stats has no rows", what)
	}
	for k, want := range stats {
		if got := views[k]; got != want {
			t.Errorf("%s[%s]: views %q, stats %q", what, k, got, want)
		}
	}
	for k, got := range views {
		if _, ok := stats[k]; !ok {
			t.Errorf("%s[%s]: views %q, missing in stats", what, k, got)
		}
	}
}
