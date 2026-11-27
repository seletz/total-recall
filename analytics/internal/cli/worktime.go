package cli

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"
)

// WorktimeJSON is one row of `worktime --json`.
type WorktimeJSON struct {
	Period         string  `json:"period"`
	Key            *string `json:"key"`
	RawSeconds     int64   `json:"raw_seconds"`
	RoundedSeconds int64   `json:"rounded_seconds"`
	Days           int64   `json:"days"`
}

// Grouping columns of analytics_worktime_v1 for worktime --by.
var worktimeBy = map[string]string{
	"category": "category",
	"project":  "project",
	"host":     "host",
	"none":     "NULL",
}

func newWorktimeCmd(a *App) *cobra.Command {
	var rf reportFlags
	cmd := &cobra.Command{
		Use:   "worktime",
		Short: "Work time per category, project or host over time",
		Long: `Work time estimated like total-recall stats --worktime (gaps between own
prompts, [worktime] of the config), over all hosts of the analytics database
together: a gap belongs to the person, not to a host. rounded_seconds is the
time to book, rounded per working day and category; it is only given for
--by category and none (0 otherwise). days counts working days with time.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			key, ok := worktimeBy[rf.by]
			if !ok {
				return usagef("--by %s: want category, project, host or none", rf.by)
			}
			saved := rf.by
			rf.by = "none" // reuse the period and range parsing
			period, _, where, args, err := rf.sql()
			rf.by = saved
			if err != nil {
				return err
			}
			rounded := "0"
			src := "analytics_worktime_v1"
			if rf.by == "category" || rf.by == "none" {
				// rounding happens per day and category
				src = "(SELECT day_local, category, raw_seconds, rounded_seconds FROM analytics_worktime_day_v1)"
				rounded = "sum(rounded_seconds)"
			}
			q := `SELECT ` + period + ` AS period, ` + key + ` AS key,
				CAST(sum(raw_seconds) AS BIGINT), CAST(` + rounded + ` AS BIGINT), count(DISTINCT day_local)
				FROM ` + src + ` WHERE ` + where + ` GROUP BY ALL ORDER BY period, key NULLS FIRST`
			out := []WorktimeJSON{}
			err = a.report(cmd.Context(), q, args, func(rows *sql.Rows) error {
				var w WorktimeJSON
				var k sql.NullString
				if err := rows.Scan(&w.Period, &k, &w.RawSeconds, &w.RoundedSeconds, &w.Days); err != nil {
					return err
				}
				w.Key = nullKey(k)
				out = append(out, w)
				return nil
			})
			if err != nil {
				return err
			}
			if rf.asJSON {
				return writeJSON(a.Stdout, out)
			}
			booked := rf.by == "category" || rf.by == "none"
			cols := []string{"DAYS", "HOURS"}
			if booked {
				cols = append(cols, "BOOKED")
			}
			t := newTable(a.Stdout, rf, cols...)
			for _, w := range out {
				cells := []string{fmt.Sprint(w.Days), clock(w.RawSeconds)}
				if booked {
					cells = append(cells, clock(w.RoundedSeconds))
				}
				t.rowText(w.Period, w.Key, cells...)
			}
			return t.flush()
		},
	}
	rf.add(cmd, "category")
	cmd.Flags().Lookup("by").Usage = "group by category, project, host or none"
	return cmd
}

// clock renders seconds as H:MM.
func clock(s int64) string {
	m := (s + 30) / 60
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}
