package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/ops"
)

// doctorFindings is the exit code of doctor when something unknown was found.
const doctorFindings = exitCode(2)

func (e *env) doctorCmd() *cobra.Command {
	var name string
	var of outputFlags
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the harness transcripts for format drift (read-only)",
		Long: `Read all transcripts of a harness (read-only, the archive is not opened)
and report what the adapter does not know: record types that are neither
ingested nor known to be skipped, content block types it does not handle,
missing expected fields, malformed lines; plus the harness versions seen.
Problems in the config tables that are only checked when used ([work],
[tui], [keys]) are reported first.

Exit code 0: nothing unknown. 2: findings (run it after a harness update).`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		var checkers []harness.FormatChecker
		for _, a := range ops.Adapters(cfg) {
			if name != "" && a.Name() != name {
				continue
			}
			if fc, ok := a.(harness.FormatChecker); ok {
				checkers = append(checkers, fc)
			}
		}
		if len(checkers) == 0 {
			return usagef("doctor: no enabled harness %q with a format check (supported: claude)", name)
		}
		reports := make([]harness.FormatReport, 0, len(checkers))
		problems := cfg.Problems()
		clean := len(problems) == 0
		if len(problems) > 0 {
			w := o.w
			if o.format.IsJSON() {
				w = o.errw
			}
			_, _ = fmt.Fprintln(w, "config problems (hooks and ingest are not affected, tui and stats --workday fail):")
			for _, p := range problems {
				_, _ = fmt.Fprintf(w, "  %s\n", p)
			}
			_, _ = fmt.Fprintln(w)
		}
		for _, fc := range checkers {
			r, err := fc.CheckFormat(cmd.Context())
			if err != nil {
				return err
			}
			reports = append(reports, r)
			clean = clean && r.Clean()
		}
		if o.format.IsJSON() {
			if err := writeList(o, reports); err != nil {
				return err
			}
		} else {
			for _, r := range reports {
				ops.FormatReport(o.w, r)
			}
		}
		if !clean {
			return doctorFindings
		}
		return nil
	})
	c.Flags().StringVar(&name, "harness", "claude", "harness to check")
	of.register(c)
	return c
}
