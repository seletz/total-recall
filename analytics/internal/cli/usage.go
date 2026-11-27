package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
)

// UsageJSON is one row of `usage --json`.
type UsageJSON struct {
	Period    string  `json:"period"`
	Key       *string `json:"key"`
	Sessions  int64   `json:"sessions"`
	Human     int64   `json:"human"`
	Agent     int64   `json:"agent"`
	ToolCalls int64   `json:"tool_calls"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
}

// TokensJSON is one row of `tokens --json`; the fields are those of
// `total-recall stats --json` (tokens).
type TokensJSON struct {
	Period     string  `json:"period"`
	Key        *string `json:"key"`
	Events     int64   `json:"events"`
	Input      int64   `json:"input"`
	Fresh      int64   `json:"fresh"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
	Unsplit    int64   `json:"unsplit"`
	Output     int64   `json:"output"`
}

// Grouping columns of analytics_events_v1 for --by.
var byColumns = map[string]string{
	"project": "project",
	"host":    "host",
	"model":   "model",
	"family":  "model_family",
	"tool":    "tool_name",
	"account": "account_name",
	"repo":    "repo",
	"harness": "harness",
	"none":    "NULL",
}

// Period expressions over day_local for --period.
var periodExprs = map[string]string{
	"day":   "strftime(day_local, '%Y-%m-%d')",
	"week":  "strftime(day_local, '%G-W%V')",
	"month": "strftime(day_local, '%Y-%m')",
	"total": "'total'",
}

// reportFlags are the flags usage and tokens share.
type reportFlags struct {
	by, period, since, until string
	asJSON                   bool
}

func (r *reportFlags) add(cmd *cobra.Command, by string) {
	f := cmd.Flags()
	f.StringVar(&r.by, "by", by, "group by project, host, model, family, tool, account, repo, harness or none")
	f.StringVar(&r.period, "period", "total", "day, week (ISO), month or total")
	f.StringVar(&r.since, "since", "", "first local day YYYY-MM-DD")
	f.StringVar(&r.until, "until", "", "last local day YYYY-MM-DD")
	f.BoolVar(&r.asJSON, "json", false, "output as JSON")
}

// sql builds the SELECT parts: period, key and the WHERE clause.
func (r *reportFlags) sql() (period, key, where string, args []any, err error) {
	var ok bool
	if key, ok = byColumns[r.by]; !ok {
		return "", "", "", nil, usagef("--by %s: want project, host, model, family, tool, account, repo, harness or none", r.by)
	}
	if period, ok = periodExprs[r.period]; !ok {
		return "", "", "", nil, usagef("--period %s: want day, week, month or total", r.period)
	}
	var conds []string
	for _, d := range []struct{ flag, val, op string }{{"since", r.since, ">="}, {"until", r.until, "<="}} {
		if d.val == "" {
			continue
		}
		if _, err := time.Parse(time.DateOnly, d.val); err != nil {
			return "", "", "", nil, usagef("--%s %s: want YYYY-MM-DD", d.flag, d.val)
		}
		conds = append(conds, "day_local "+d.op+" CAST(? AS DATE)")
		args = append(args, d.val)
	}
	where = "TRUE"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	return period, key, where, args, nil
}

func newUsageCmd(a *App) *cobra.Command {
	var rf reportFlags
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Agent usage per project, host, model or tool over time",
		Long: `Sessions, own prompts (human), agent events, tool calls and tokens, grouped by
--by and --period in the primary time zone. sessions counts the sessions with
a human or agent event in the group, as stats does.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			period, key, where, args, err := rf.sql()
			if err != nil {
				return err
			}
			q := `SELECT ` + period + ` AS period, ` + key + ` AS key,
				count(DISTINCT CASE WHEN actor IN ('human', 'agent') THEN session_id END),
				count(*) FILTER (WHERE actor = 'human'),
				count(*) FILTER (WHERE actor = 'agent'),
				count(*) FILTER (WHERE kind = 'tool_use'),
				CAST(COALESCE(sum(tokens_in), 0) AS BIGINT), CAST(COALESCE(sum(tokens_out), 0) AS BIGINT)
				FROM analytics_events_v1 WHERE ` + where + `
				GROUP BY ALL ORDER BY period, key NULLS FIRST`
			out := []UsageJSON{}
			err = a.report(cmd.Context(), q, args, func(rows *sql.Rows) error {
				var u UsageJSON
				var k sql.NullString
				if err := rows.Scan(&u.Period, &k, &u.Sessions, &u.Human, &u.Agent, &u.ToolCalls, &u.TokensIn, &u.TokensOut); err != nil {
					return err
				}
				u.Key = nullKey(k)
				out = append(out, u)
				return nil
			})
			if err != nil {
				return err
			}
			if rf.asJSON {
				return writeJSON(a.Stdout, out)
			}
			t := newTable(a.Stdout, rf, "SESSIONS", "HUMAN", "AGENT", "TOOL CALLS", "TOKENS IN", "TOKENS OUT")
			for _, u := range out {
				t.row(u.Period, u.Key, u.Sessions, u.Human, u.Agent, u.ToolCalls, u.TokensIn, u.TokensOut)
			}
			return t.flush()
		},
	}
	rf.add(cmd, "project")
	return cmd
}

func newTokensCmd(a *App) *cobra.Command {
	var rf reportFlags
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Tokens per model, project or host over time, with the cache split",
		Long: `Events with token usage and their tokens, grouped by --by and --period: input
(= fresh + cache read + cache write + unsplit) and output, the fields of
total-recall stats --json.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			period, key, where, args, err := rf.sql()
			if err != nil {
				return err
			}
			q := `SELECT ` + period + ` AS period, ` + key + ` AS key, count(*),
				CAST(sum(tokens_in) AS BIGINT), CAST(sum(tokens_fresh) AS BIGINT), CAST(sum(tokens_cache_read) AS BIGINT),
				CAST(sum(tokens_cache_write) AS BIGINT), CAST(sum(tokens_unsplit) AS BIGINT), CAST(sum(tokens_out) AS BIGINT)
				FROM analytics_events_v1 WHERE (tokens_in > 0 OR tokens_out > 0) AND ` + where + `
				GROUP BY ALL ORDER BY period, key NULLS FIRST`
			out := []TokensJSON{}
			err = a.report(cmd.Context(), q, args, func(rows *sql.Rows) error {
				var t TokensJSON
				var k sql.NullString
				if err := rows.Scan(&t.Period, &k, &t.Events, &t.Input, &t.Fresh, &t.CacheRead, &t.CacheWrite, &t.Unsplit, &t.Output); err != nil {
					return err
				}
				t.Key = nullKey(k)
				out = append(out, t)
				return nil
			})
			if err != nil {
				return err
			}
			if rf.asJSON {
				return writeJSON(a.Stdout, out)
			}
			tb := newTable(a.Stdout, rf, "EVENTS", "INPUT", "FRESH", "CACHE READ", "CACHE WRITE", "UNSPLIT", "OUTPUT")
			for _, t := range out {
				tb.row(t.Period, t.Key, t.Events, t.Input, t.Fresh, t.CacheRead, t.CacheWrite, t.Unsplit, t.Output)
			}
			return tb.flush()
		},
	}
	rf.add(cmd, "model")
	return cmd
}

// report runs a query against the analytics database and scans each row.
func (a *App) report(ctx context.Context, q string, args []any, scan func(*sql.Rows) error) error {
	cfg, err := a.config()
	if err != nil {
		return err
	}
	db, _, err := a.openAnalytics(ctx, cfg, true)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return queryRows(ctx, db, q, args, scan)
}

func queryRows(ctx context.Context, db *duck.DB, q string, args []any, scan func(*sql.Rows) error) error {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return errKind(ErrKindSQL, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return errKind(ErrKindSQL, err)
		}
	}
	return errKind(ErrKindSQL, rows.Err())
}

// nullKey maps NULL and "" to a JSON null.
func nullKey(k sql.NullString) *string {
	if !k.Valid || k.String == "" {
		return nil
	}
	return &k.String
}

func keyText(k *string) string {
	if k == nil {
		return "-"
	}
	return *k
}

// table writes a report: period and key left, numbers right aligned; the
// key column is left out for --by none.
type table struct {
	w      io.Writer
	noKey  bool
	header []string
	rows   [][]string
}

func newTable(w io.Writer, rf reportFlags, cols ...string) *table {
	t := &table{w: w, noKey: rf.by == "none"}
	t.header = []string{strings.ToUpper(rf.period)}
	if !t.noKey {
		t.header = append(t.header, strings.ToUpper(rf.by))
	}
	t.header = append(t.header, cols...)
	return t
}

func (t *table) row(period string, key *string, nums ...int64) {
	r := []string{period}
	if !t.noKey {
		r = append(r, keyText(key))
	}
	for _, n := range nums {
		r = append(r, fmt.Sprint(n))
	}
	t.rows = append(t.rows, r)
}

func (t *table) flush() error {
	text := 1
	if !t.noKey {
		text = 2
	}
	width := make([]int, len(t.header))
	for _, r := range append([][]string{t.header}, t.rows...) {
		for i, c := range r {
			width[i] = max(width[i], utf8.RuneCountInString(c))
		}
	}
	for _, r := range append([][]string{t.header}, t.rows...) {
		cells := make([]string, len(r))
		for i, c := range r {
			pad := strings.Repeat(" ", width[i]-utf8.RuneCountInString(c))
			if i < text {
				cells[i] = c + pad
			} else {
				cells[i] = pad + c
			}
		}
		if _, err := fmt.Fprintln(t.w, strings.TrimRight(strings.Join(cells, "  "), " ")); err != nil {
			return err
		}
	}
	return nil
}
