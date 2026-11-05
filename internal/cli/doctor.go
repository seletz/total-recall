package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

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
				printFormatReport(o.w, r)
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

func printFormatReport(w io.Writer, r harness.FormatReport) {
	p := func(f string, a ...any) { _, _ = fmt.Fprintf(w, f, a...) }
	p("harness %s: %d sources, %d lines\n", r.Harness, r.Sources, r.Lines)
	vs := make([]string, 0, len(r.Versions))
	for i, v := range r.Versions {
		if i == 8 {
			vs = append(vs, fmt.Sprintf("… (%d more)", len(r.Versions)-i))
			break
		}
		vs = append(vs, v.Version+" ("+strconv.FormatInt(v.Count, 10)+")")
	}
	if len(vs) == 0 {
		vs = []string{"none"}
	}
	p("versions (newest first): %s\n", strings.Join(vs, ", "))
	section := func(title string, m map[string]*harness.Finding) {
		if len(m) == 0 {
			return
		}
		p("\n%s:\n", title)
		for _, k := range sortedKeys(m) {
			p("  %-32s %8d  first: %s\n", k, m[k].Count, m[k].Example)
		}
	}
	section("unknown record types", r.UnknownRecordTypes)
	section("unknown content block types", r.UnknownBlockTypes)
	if len(r.MissingFields) > 0 {
		flat := map[string]*harness.Finding{}
		for typ, fields := range r.MissingFields {
			for f, v := range fields {
				flat[typ+"."+f] = v
			}
		}
		section("missing fields (record type.field)", flat)
	}
	if r.Malformed.Count > 0 {
		p("\nmalformed lines: %d  first: %s\n", r.Malformed.Count, r.Malformed.Example)
	}
	if r.Clean() {
		p("no unknown record types, content blocks or missing fields\n")
	} else {
		p("\nfindings: the adapter may need an update (see docs: Hooks, \"Nach einem Claude-Code-Update\")\n")
	}
}

func sortedKeys(m map[string]*harness.Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
