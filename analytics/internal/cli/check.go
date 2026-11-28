package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/star"
)

// CheckJSON is the output of `check --json`.
type CheckJSON struct {
	AnalyticsDB  string     `json:"analytics_db"`
	OK           bool       `json:"ok"`
	ContractOK   bool       `json:"contract_ok"`
	ContractDiff []string   `json:"contract_diff"`
	Tests        []TestJSON `json:"tests"`
}

// TestJSON is one data test; failures counts the violating rows.
type TestJSON struct {
	Name     string `json:"name"`
	Failures int    `json:"failures"`
}

func newCheckCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check the contract of the views and run the data tests",
		Long: `Checks the published views against their contract and runs the data tests
on the analytics database, without loading anything. Exit code 1 when one
fails, for CI and cron.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := a.config()
			if err != nil {
				return err
			}
			db, path, err := a.openAnalytics(ctx, cfg, true)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			r, err := star.Check(ctx, db)
			if err != nil {
				return errKind(ErrKindDB, err)
			}
			out := CheckJSON{AnalyticsDB: path, OK: r.OK(), ContractOK: r.ContractOK, ContractDiff: append([]string{}, r.Diff...), Tests: []TestJSON{}}
			for _, t := range r.Tests {
				out.Tests = append(out.Tests, TestJSON{Name: t.Name, Failures: t.Failures})
			}
			if asJSON {
				if err := writeJSON(a.Stdout, out); err != nil {
					return err
				}
			} else {
				state := map[bool]string{true: "ok", false: "FAILED"}
				tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintf(tw, "contract\t%s\n", state[out.ContractOK])
				for _, d := range out.ContractDiff {
					_, _ = fmt.Fprintf(tw, "  %s\t\n", d)
				}
				for _, t := range out.Tests {
					s := state[t.Failures == 0]
					if t.Failures > 0 {
						s = fmt.Sprintf("FAILED (%d rows)", t.Failures)
					}
					_, _ = fmt.Fprintf(tw, "%s\t%s\n", t.Name, s)
				}
				if err := tw.Flush(); err != nil {
					return err
				}
			}
			if !out.OK {
				return exitCode(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
