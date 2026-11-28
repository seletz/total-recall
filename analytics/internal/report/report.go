// Package report holds the queries of the analytics tool over the
// published views: usage, tokens and work time grouped by a key and a
// period, with a range of days and a host filter. The commands usage,
// tokens and worktime and the TUI run the same queries, so their numbers
// are the same.
package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrFilter wraps every error about a Filter value (a usage error of the
// CLI).
var ErrFilter = errors.New("invalid filter")

// Querier runs a query; *duck.DB and *sql.DB are Queriers.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Filter selects and groups the rows of a report.
type Filter struct {
	// By is the grouping: see UsageBy and WorktimeBy.
	By string
	// Period is day, week (ISO), month or total.
	Period string
	// Since and Until are the first and the last local day (YYYY-MM-DD),
	// empty for an open end.
	Since, Until string
	// Hosts keeps the rows of these hosts; empty keeps every host.
	Hosts []string
}

// UsageBy are the groupings of Usage and Tokens, by name, with their
// column of analytics_events_v1.
var UsageBy = map[string]string{
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

// UsageByNames lists UsageBy in the order of the help texts.
var UsageByNames = []string{"project", "host", "model", "family", "tool", "account", "repo", "harness", "none"}

// WorktimeBy are the groupings of Worktime with their column of
// analytics_worktime_v1.
var WorktimeBy = map[string]string{
	"category": "category",
	"project":  "project",
	"host":     "host",
	"none":     "NULL",
}

// WorktimeByNames lists WorktimeBy in the order of the help texts.
var WorktimeByNames = []string{"category", "project", "host", "none"}

// Periods are the period expressions over day_local.
var Periods = map[string]string{
	"day":   "strftime(day_local, '%Y-%m-%d')",
	"week":  "strftime(day_local, '%G-W%V')",
	"month": "strftime(day_local, '%Y-%m')",
	"total": "'total'",
}

// PeriodNames lists Periods in the order of the help texts.
var PeriodNames = []string{"day", "week", "month", "total"}

// where builds the period expression and the WHERE clause of f.
func (f Filter) where() (period, where string, args []any, err error) {
	period, ok := Periods[f.Period]
	if !ok {
		return "", "", nil, fmt.Errorf("%w: period %s: want %s", ErrFilter, f.Period, strings.Join(PeriodNames, ", "))
	}
	conds := []string{"TRUE"}
	for _, d := range []struct{ name, val, op string }{{"since", f.Since, ">="}, {"until", f.Until, "<="}} {
		if d.val == "" {
			continue
		}
		if _, err := time.Parse(time.DateOnly, d.val); err != nil {
			return "", "", nil, fmt.Errorf("%w: %s %s: want YYYY-MM-DD", ErrFilter, d.name, d.val)
		}
		conds = append(conds, "day_local "+d.op+" CAST(? AS DATE)")
		args = append(args, d.val)
	}
	if len(f.Hosts) > 0 {
		conds = append(conds, "host IN ("+strings.TrimSuffix(strings.Repeat("?, ", len(f.Hosts)), ", ")+")")
		for _, h := range f.Hosts {
			args = append(args, h)
		}
	}
	return period, strings.Join(conds, " AND "), args, nil
}

func key(by string, cols map[string]string, names []string) (string, error) {
	k, ok := cols[by]
	if !ok {
		return "", fmt.Errorf("%w: by %s: want %s", ErrFilter, by, strings.Join(names, ", "))
	}
	return k, nil
}

// Usage is one row of a usage report.
type Usage struct {
	Period    string  `json:"period"`
	Key       *string `json:"key"`
	Sessions  int64   `json:"sessions"`
	Human     int64   `json:"human"`
	Agent     int64   `json:"agent"`
	ToolCalls int64   `json:"tool_calls"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
}

// UsageRows reports sessions, own prompts, agent events, tool calls and
// tokens. Sessions counts the sessions with a human or agent event in the
// group, as stats does.
func UsageRows(ctx context.Context, db Querier, f Filter) ([]Usage, error) {
	k, err := key(f.By, UsageBy, UsageByNames)
	if err != nil {
		return nil, err
	}
	period, where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + period + ` AS period, ` + k + ` AS key,
		count(DISTINCT CASE WHEN actor IN ('human', 'agent') THEN session_id END),
		count(*) FILTER (WHERE actor = 'human'),
		count(*) FILTER (WHERE actor = 'agent'),
		count(*) FILTER (WHERE kind = 'tool_use'),
		CAST(COALESCE(sum(tokens_in), 0) AS BIGINT), CAST(COALESCE(sum(tokens_out), 0) AS BIGINT)
		FROM analytics_events_v1 WHERE ` + where + `
		GROUP BY ALL ORDER BY period, key NULLS FIRST`
	out := []Usage{}
	err = scanRows(ctx, db, q, args, func(rows *sql.Rows) error {
		var u Usage
		var k sql.NullString
		if err := rows.Scan(&u.Period, &k, &u.Sessions, &u.Human, &u.Agent, &u.ToolCalls, &u.TokensIn, &u.TokensOut); err != nil {
			return err
		}
		u.Key = nullKey(k)
		out = append(out, u)
		return nil
	})
	return out, err
}

// Tokens is one row of a tokens report; the fields are those of
// `total-recall stats --json` (tokens).
type Tokens struct {
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

// TokensRows reports the events with token usage and their tokens.
func TokensRows(ctx context.Context, db Querier, f Filter) ([]Tokens, error) {
	k, err := key(f.By, UsageBy, UsageByNames)
	if err != nil {
		return nil, err
	}
	period, where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + period + ` AS period, ` + k + ` AS key, count(*),
		CAST(sum(tokens_in) AS BIGINT), CAST(sum(tokens_fresh) AS BIGINT), CAST(sum(tokens_cache_read) AS BIGINT),
		CAST(sum(tokens_cache_write) AS BIGINT), CAST(sum(tokens_unsplit) AS BIGINT), CAST(sum(tokens_out) AS BIGINT)
		FROM analytics_events_v1 WHERE (tokens_in > 0 OR tokens_out > 0) AND ` + where + `
		GROUP BY ALL ORDER BY period, key NULLS FIRST`
	out := []Tokens{}
	err = scanRows(ctx, db, q, args, func(rows *sql.Rows) error {
		var t Tokens
		var k sql.NullString
		if err := rows.Scan(&t.Period, &k, &t.Events, &t.Input, &t.Fresh, &t.CacheRead, &t.CacheWrite, &t.Unsplit, &t.Output); err != nil {
			return err
		}
		t.Key = nullKey(k)
		out = append(out, t)
		return nil
	})
	return out, err
}

// Worktime is one row of a work time report.
type Worktime struct {
	Period         string  `json:"period"`
	Key            *string `json:"key"`
	RawSeconds     int64   `json:"raw_seconds"`
	RoundedSeconds int64   `json:"rounded_seconds"`
	Days           int64   `json:"days"`
}

// Booked reports whether a work time report of f has the rounded time to
// book: rounding happens per working day and category over all hosts, so
// only the groupings category and none of every host have it.
func Booked(f Filter) bool { return (f.By == "category" || f.By == "none") && len(f.Hosts) == 0 }

// WorktimeRows reports the work time (see Booked for RoundedSeconds); days
// counts the working days with time.
func WorktimeRows(ctx context.Context, db Querier, f Filter) ([]Worktime, error) {
	k, err := key(f.By, WorktimeBy, WorktimeByNames)
	if err != nil {
		return nil, err
	}
	period, where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	rounded, src := "0", "analytics_worktime_v1"
	if Booked(f) {
		src = "(SELECT day_local, category, raw_seconds, rounded_seconds FROM analytics_worktime_day_v1)"
		rounded = "sum(rounded_seconds)"
	}
	q := `SELECT ` + period + ` AS period, ` + k + ` AS key,
		CAST(sum(raw_seconds) AS BIGINT), CAST(` + rounded + ` AS BIGINT), count(DISTINCT day_local)
		FROM ` + src + ` WHERE ` + where + ` GROUP BY ALL ORDER BY period, key NULLS FIRST`
	out := []Worktime{}
	err = scanRows(ctx, db, q, args, func(rows *sql.Rows) error {
		var w Worktime
		var k sql.NullString
		if err := rows.Scan(&w.Period, &k, &w.RawSeconds, &w.RoundedSeconds, &w.Days); err != nil {
			return err
		}
		w.Key = nullKey(k)
		out = append(out, w)
		return nil
	})
	return out, err
}

// Hosts returns the hosts with sessions in the analytics database, sorted.
func Hosts(ctx context.Context, db Querier) ([]string, error) {
	var out []string
	err := scanRows(ctx, db, "SELECT DISTINCT host FROM analytics_sessions_v1 WHERE host IS NOT NULL ORDER BY host", nil, func(rows *sql.Rows) error {
		var h string
		if err := rows.Scan(&h); err != nil {
			return err
		}
		out = append(out, h)
		return nil
	})
	slices.Sort(out)
	return out, err
}

// Days returns the first and the last local day with an event, as
// YYYY-MM-DD; both empty without events.
func Days(ctx context.Context, db Querier) (first, last string, err error) {
	err = scanRows(ctx, db, "SELECT COALESCE(strftime(min(day_local), '%Y-%m-%d'), ''), COALESCE(strftime(max(day_local), '%Y-%m-%d'), '') FROM analytics_events_v1", nil, func(rows *sql.Rows) error {
		return rows.Scan(&first, &last)
	})
	return first, last, err
}

func scanRows(ctx context.Context, db Querier, q string, args []any, scan func(*sql.Rows) error) error {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// nullKey maps NULL and "" to nil (JSON null).
func nullKey(k sql.NullString) *string {
	if !k.Valid || k.String == "" {
		return nil
	}
	return &k.String
}
