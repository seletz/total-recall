package star

import (
	"bytes"
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

// withTokens gives the demo copy token counts and three models; the demo
// archive has neither yet (#365). Deterministic from seq; every fifth event
// has no cache split, so all four input parts occur.
const withTokens = `UPDATE events SET
	tokens_in = 1000 + seq % 997,
	tokens_out = 50 + seq % 331,
	tokens_cache_read = CASE WHEN seq % 5 = 0 THEN NULL ELSE 400 + seq % 401 END,
	tokens_cache_write = CASE WHEN seq % 5 = 0 THEN NULL ELSE seq % 97 END,
	model = CASE seq % 3 WHEN 0 THEN 'kompass-model-opus-x' WHEN 1 THEN 'kompass-model-sonnet-y' ELSE 'kompass-model-haiku-z' END
 WHERE role = 'assistant' AND kind = 'message'`

// statsJSON runs `total-recall stats --json` on the archive, with the demo
// home and Europe/Berlin, the same function the CLI uses.
func statsJSON(t *testing.T, archive string, args ...string) map[string]any {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, "db = %q\ntimezone = \"Europe/Berlin\"\n\n[git]\nresolve = false\n", archive), 0o644); err != nil {
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

type keyed = map[string]string

// byKey turns "k|a|b" rows into k -> "a|b".
func byKey(rows []string) keyed {
	out := keyed{}
	for _, r := range rows {
		k, v, _ := strings.Cut(r, "|")
		out[k] = v
	}
	return out
}

func num(v any) string { return fmt.Sprintf("%.0f", v.(float64)) }

// fromJSON maps a list of JSON objects to "a|b" strings keyed by a field.
func fromJSON(list any, key string, fields ...string) keyed {
	out := keyed{}
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

// TestSumEqualsParts compares the published views with `stats --json` on
// the same archive: totals, per day, per repo, per weekday and hour, tokens
// in total, per day and per model, tool calls per tool and per project.
func TestSumEqualsParts(t *testing.T) {
	archive := demoCopy(t)
	execSQLite(t, archive, withTokens)
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	mustRun(t, db, opts(t, archive))

	st := statsJSON(t, archive)
	ev := "FROM analytics_events_v1 WHERE actor IN ('human', 'agent')"
	act := "count(DISTINCT session_id), count(*) FILTER (WHERE actor = 'human'), count(*) FILTER (WHERE actor = 'agent')"

	total := st["total"].(map[string]any)
	if got, want := row(t, db, "SELECT "+act+" "+ev), num(total["sessions"])+"|"+num(total["human"])+"|"+num(total["agent"]); got != want {
		t.Errorf("total: views %s, stats %s", got, want)
	}
	compare(t, "by_day", byKey(table(t, db, "SELECT strftime(day_local, '%Y-%m-%d'), "+act+" "+ev+" GROUP BY day_local")),
		fromJSON(st["by_day"], "day", "sessions", "human", "agent"))
	compare(t, "by_day (daily_v1)", byKey(table(t, db, "SELECT strftime(day_local, '%Y-%m-%d'), sum(human_prompts), sum(agent_events) FROM analytics_daily_v1 GROUP BY day_local")),
		fromJSON(st["by_day"], "day", "human", "agent"))
	compare(t, "by_repo", byKey(table(t, db, "SELECT repo, "+act+" "+ev+" GROUP BY repo")),
		fromJSON(st["by_repo"], "repo", "sessions", "human", "agent"))

	wh, whEvents := keyed{}, keyed{}
	for _, it := range st["by_weekday_hour"].([]any) {
		o := it.(map[string]any)
		if o["human"].(float64)+o["agent"].(float64) == 0 {
			continue
		}
		k := fmt.Sprintf("%d-%s", int(o["weekday"].(float64))+1, num(o["hour"]))
		wh[k] = num(o["sessions"]) + "|" + num(o["human"]) + "|" + num(o["agent"])
		whEvents[k] = num(o["human"]) + "|" + num(o["agent"])
	}
	compare(t, "by_weekday_hour", byKey(table(t, db, "SELECT weekday_local || '-' || hour_local, "+act+" "+ev+" GROUP BY weekday_local, hour_local")), wh)
	compare(t, "by_weekday_hour (weekhour_v1)", byKey(table(t, db, "SELECT iso_weekday || '-' || local_hour, human_events, agent_events FROM analytics_weekhour_v1")), whEvents)

	tok := st["tokens"].(map[string]any)
	if num(tok["input"]) == "0" {
		t.Fatal("token injection did not reach stats")
	}
	tokFields := []string{"events", "input", "fresh", "cache_read", "cache_write", "unsplit", "output"}
	tokCols := "sum(events), sum(tokens_in), sum(tokens_fresh), sum(tokens_cache_read), sum(tokens_cache_write), sum(tokens_unsplit), sum(tokens_out)"
	totalTok := make([]string, len(tokFields))
	for i, f := range tokFields {
		totalTok[i] = num(tok[f])
	}
	compare(t, "tokens total", byKey(table(t, db, "SELECT 'total', "+tokCols+" FROM analytics_tokens_day_v1")), keyed{"total": strings.Join(totalTok, "|")})
	compare(t, "tokens by_day", byKey(table(t, db, "SELECT strftime(day_local, '%Y-%m-%d'), "+tokCols+" FROM analytics_tokens_day_v1 GROUP BY day_local")),
		fromJSON(tok["by_day"], "day", tokFields...))
	compare(t, "tokens by_model", byKey(table(t, db, "SELECT model, "+tokCols+" FROM analytics_tokens_day_v1 GROUP BY model")),
		fromJSON(tok["by_model"], "model", tokFields...))

	tools := statsJSON(t, archive, "--tools", "--by", "project")
	compare(t, "tools total", byKey(table(t, db, "SELECT 'total', sum(calls) FROM analytics_tool_month_v1")), keyed{"total": num(tools["total"])})
	compare(t, "tools by_tool", byKey(table(t, db, "SELECT tool_name, sum(calls) FROM analytics_tool_month_v1 GROUP BY tool_name")),
		fromJSON(tools["by_tool"], "tool", "count"))
	// --json keeps a directory project as its full path; the analytics
	// database abbreviates home to "~" (no user name, one key across hosts).
	byProject := keyed{}
	for k, v := range fromJSON(tools["groups"], "key", "count") {
		byProject[Tilde(k, demo.Home)] = v
	}
	compare(t, "tools by project", byKey(table(t, db, "SELECT project, sum(calls) FROM analytics_tool_month_v1 GROUP BY project")), byProject)
}

// TestWorktimeSumEqualsParts compares the work-time views with
// stats --worktime --json: per day and category credited and rounded
// seconds, per day and project credited seconds, and the total.
func TestWorktimeSumEqualsParts(t *testing.T) {
	archive := demoCopy(t)
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	mustRun(t, db, opts(t, archive))
	st := statsJSON(t, archive, "--worktime")

	cells, projects := keyed{}, keyed{}
	for _, it := range st["days"].([]any) {
		d := it.(map[string]any)
		for _, ci := range d["categories"].([]any) {
			c := ci.(map[string]any)
			cells[d["day"].(string)+" "+c["category"].(string)] = num(c["raw_seconds"]) + "|" + num(c["rounded_seconds"])
			for _, pi := range c["projects"].([]any) {
				p := pi.(map[string]any)
				k := d["day"].(string) + " " + Tilde(p["project"].(string), demo.Home)
				projects[k] = num(p["raw_seconds"])
			}
		}
	}
	compare(t, "worktime day x category",
		byKey(table(t, db, "SELECT strftime(day_local, '%Y-%m-%d') || ' ' || category, raw_seconds, rounded_seconds FROM analytics_worktime_day_v1")), cells)
	compare(t, "worktime day x project",
		byKey(table(t, db, "SELECT strftime(day_local, '%Y-%m-%d') || ' ' || project, sum(raw_seconds) FROM analytics_worktime_v1 GROUP BY ALL")), projects)
	tot := st["total"].(map[string]any)
	if got, want := row(t, db, "SELECT sum(raw_seconds), sum(rounded_seconds), count(DISTINCT day_local) FROM analytics_worktime_day_v1"),
		num(tot["raw_seconds"])+"|"+num(tot["rounded_seconds"])+"|"+num(tot["days"]); got != want {
		t.Errorf("worktime total: views %s, stats %s", got, want)
	}
}
