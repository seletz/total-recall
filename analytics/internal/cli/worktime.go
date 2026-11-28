package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/report"
)

// WorktimeJSON is one row of `worktime --json`.
type WorktimeJSON = report.Worktime

func newWorktimeCmd(a *App) *cobra.Command {
	var rf reportFlags
	cmd := &cobra.Command{
		Use:   "worktime",
		Short: "Work time per category, project or host over time",
		Long: `Work time estimated like total-recall stats --worktime (gaps between own
prompts, [worktime] of the config), over all hosts of the analytics database
together: a gap belongs to the person, not to a host. rounded_seconds is the
time to book, rounded per working day and category over all hosts; it is
only given for --by category and none without --host (0 otherwise). days
counts working days with time.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var out []WorktimeJSON
			err := a.withReport(cmd.Context(), func(db report.Querier) (err error) {
				out, err = report.WorktimeRows(cmd.Context(), db, rf.Filter)
				return err
			})
			if err != nil {
				return err
			}
			if rf.asJSON {
				return writeJSON(a.Stdout, out)
			}
			booked := report.Booked(rf.Filter)
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
	rf.add(cmd, "category", report.WorktimeByNames)
	return cmd
}

// clock renders seconds as H:MM.
func clock(s int64) string {
	m := (s + 30) / 60
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}
