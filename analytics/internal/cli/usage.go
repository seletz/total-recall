package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/report"
)

// UsageJSON is one row of `usage --json`.
type UsageJSON = report.Usage

// TokensJSON is one row of `tokens --json`; the fields are those of
// `total-recall stats --json` (tokens).
type TokensJSON = report.Tokens

// reportFlags are the flags usage, tokens and worktime share.
type reportFlags struct {
	report.Filter
	asJSON bool
}

func (r *reportFlags) add(cmd *cobra.Command, by string, names []string) {
	f := cmd.Flags()
	f.StringVar(&r.By, "by", by, "group by "+orList(names))
	f.StringVar(&r.Period, "period", "total", "day, week (ISO), month or total")
	f.StringVar(&r.Since, "since", "", "first local day YYYY-MM-DD")
	f.StringVar(&r.Until, "until", "", "last local day YYYY-MM-DD")
	f.StringSliceVar(&r.Hosts, "host", nil, "only these hosts (repeatable)")
	f.BoolVar(&r.asJSON, "json", false, "output as JSON")
}

// orList joins names as "a, b or c".
func orList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// reportErr maps the errors of a report: a bad filter is a usage error,
// the rest an error of the query.
func reportErr(err error) error {
	if errors.Is(err, report.ErrFilter) {
		return usagef("%s", strings.TrimPrefix(err.Error(), report.ErrFilter.Error()+": "))
	}
	return errKind(ErrKindSQL, err)
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
			var out []UsageJSON
			err := a.withReport(cmd.Context(), func(db report.Querier) (err error) {
				out, err = report.UsageRows(cmd.Context(), db, rf.Filter)
				return err
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
	rf.add(cmd, "project", report.UsageByNames)
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
			var out []TokensJSON
			err := a.withReport(cmd.Context(), func(db report.Querier) (err error) {
				out, err = report.TokensRows(cmd.Context(), db, rf.Filter)
				return err
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
	rf.add(cmd, "model", report.UsageByNames)
	return cmd
}

// withReport opens the analytics database read-only and runs a report.
func (a *App) withReport(ctx context.Context, run func(report.Querier) error) error {
	cfg, err := a.config()
	if err != nil {
		return err
	}
	db, _, err := a.openAnalytics(ctx, cfg, true)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if err := run(db); err != nil {
		return reportErr(err)
	}
	return nil
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
	t := &table{w: w, noKey: rf.By == "none"}
	t.header = []string{strings.ToUpper(rf.Period)}
	if !t.noKey {
		t.header = append(t.header, strings.ToUpper(rf.By))
	}
	t.header = append(t.header, cols...)
	return t
}

func (t *table) row(period string, key *string, nums ...int64) {
	cells := make([]string, len(nums))
	for i, n := range nums {
		cells[i] = fmt.Sprint(n)
	}
	t.rowText(period, key, cells...)
}

// rowText adds a row with preformatted value cells (right aligned).
func (t *table) rowText(period string, key *string, cells ...string) {
	r := []string{period}
	if !t.noKey {
		r = append(r, keyText(key))
	}
	t.rows = append(t.rows, append(r, cells...))
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
