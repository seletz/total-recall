package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/worktime"
)

// doctorFindings is the exit code of doctor when something unknown was found.
const doctorFindings = exitCode(2)

func (e *env) doctorCmd() *cobra.Command {
	var name string
	var days int
	var of outputFlags
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the harness transcripts for format drift (read-only)",
		Long: `Read all transcripts of a harness (read-only, the archive is not opened)
and report what the adapter does not know: record types that are neither
ingested nor known to be skipped, content block types it does not handle,
missing expected fields, malformed lines; plus the harness versions seen.
Problems in the config tables that are only checked when used ([work],
[tui], [keys]) are reported first. Then a hint (no finding, the exit code
stays) names the projects with your own messages in the last --days days
(default 30) that no [[project]] matches or that no [[worktime.category]]
covers, so a new repository does not go unnoticed until booking time. Each
check applies only if the table has entries; --days 0 skips the hint.

A second hint names a backup directory that lies in an iCloud / file provider
domain (macOS: the marker on the directory or a parent), where every backup is
synced; it is the field "backup_hint" with --json (null otherwise).
With --json the hint is the field "hints" of every report, apart from the
findings: project, key, missing ("project" and/or "category") and
last_active.

Exit code 0: nothing unknown. 2: findings (run it after a harness update).
Hints are no findings and never change the exit code.`,
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
		hints := []setupHint{}
		if days > 0 {
			var err error
			hints, err = setupHints(cmd.Context(), cfg, e.app.Now(), days)
			if err != nil {
				return err
			}
			if len(hints) > 0 {
				w := o.w
				if o.format.IsJSON() {
					w = o.errw
				}
				writeSetupHints(w, hints, days)
			}
		}
		syncHint := backupSyncHint(cfg.Backup.Dir, e.app.FileProvider)
		if syncHint != nil {
			w := o.w
			if o.format.IsJSON() {
				w = o.errw
			}
			writeBackupSyncHint(w, syncHint)
		}
		if note := schemaNote(cfg); note != "" {
			clean = false
			w := o.w
			if o.format.IsJSON() {
				w = o.errw
			}
			_, _ = fmt.Fprintf(w, "%s\n\n", note)
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
			out := make([]DoctorReportJSON, len(reports))
			for i, r := range reports {
				out[i] = DoctorReportJSON{FormatReport: r, Hints: doctorHintsJSON(hints), BackupHint: syncHint}
			}
			if err := writeList(o, out); err != nil {
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
	c.Flags().IntVar(&days, "days", 30, "hint at projects with activity in the last N days that have no [[project]] or no category (0 = off)")
	of.register(c)
	return c
}

// schemaNote returns a loud note when the archive exists and needs a
// migration (or was written by a newer binary), else "". doctor does not
// open the archive otherwise; the note is read-only.
func schemaNote(cfg *config.Config) string {
	if _, err := os.Stat(cfg.DB); err != nil {
		return ""
	}
	st, err := store.OpenReadOnly(cfg.DB)
	if err != nil {
		if errors.Is(err, store.ErrSchemaTooNew) {
			return "SCHEMA: " + err.Error()
		}
		return ""
	}
	defer func() { _ = st.Close() }()
	if !st.MigrationPending() {
		return ""
	}
	return fmt.Sprintf("MIGRATION PENDING: the archive has schema v%d, this binary v%d; hooks write nothing until you %s",
		st.Version(), store.LatestSchemaVersion(), migrateHint)
}

// setupHint is a project with recent activity that the config does not
// fully cover.
type setupHint struct {
	name, key             string
	noProject, noCategory bool
	last                  time.Time
}

// DoctorReportJSON is one element of doctor --json: the findings of a
// harness (FormatReport, unchanged) and, apart from them, the hints about
// projects the config does not fully cover. The hints are no findings: they
// never change the exit code. They are the same in every element.
type DoctorReportJSON struct {
	harness.FormatReport
	Hints []DoctorHintJSON `json:"hints"`
	// BackupHint is set when the backup directory lies in an iCloud / file
	// provider domain, null otherwise. Like Hints it is no finding.
	BackupHint *DoctorBackupHintJSON `json:"backup_hint"`
}

// DoctorHintJSON is a project with recent own messages that the config does
// not fully cover.
type DoctorHintJSON struct {
	Project string `json:"project"`
	Key     string `json:"key"`
	// Missing names what is missing: "project" (no [[project]] matches),
	// "category" (no [[worktime.category]] covers it), or both.
	Missing []string `json:"missing"`
	// LastActive is the time of the latest own message of the project (UTC).
	LastActive string `json:"last_active"`
}

func doctorHintsJSON(hints []setupHint) []DoctorHintJSON {
	out := make([]DoctorHintJSON, len(hints))
	for i, h := range hints {
		out[i] = DoctorHintJSON{Project: h.name, Key: h.key, Missing: []string{}, LastActive: h.last.UTC().Format(time.RFC3339)}
		if h.noProject {
			out[i].Missing = append(out[i].Missing, "project")
		}
		if h.noCategory {
			out[i].Missing = append(out[i].Missing, "category")
		}
	}
	return out
}

// setupHints lists the projects with own messages in the last days days
// that no [[project]] matches (only if [[project]] entries exist) or that
// no [[worktime.category]] covers (only if categories exist), by name. The
// categories are those of stats --worktime: the report of the same events
// names the projects without one. Nothing is returned when the archive is
// missing, unreadable or needs a migration: doctor does not depend on it.
func setupHints(ctx context.Context, cfg *config.Config, now time.Time, days int) ([]setupHint, error) {
	if _, err := os.Stat(cfg.DB); err != nil {
		return nil, nil
	}
	settings, err := cfg.WorktimeSettings()
	if err != nil {
		return nil, nil // reported as a config problem already
	}
	res, err := cfg.ProjectResolver()
	if err != nil {
		return nil, nil
	}
	checkProjects, checkCategories := !res.Empty(), len(settings.Rules) > 0
	if !checkProjects && !checkCategories {
		return nil, nil
	}
	st, err := store.OpenReadOnly(cfg.DB, store.WithLocation(cfg.Location()))
	if err != nil {
		return nil, nil
	}
	defer func() { _ = st.Close() }()
	if st.MigrationPending() {
		return nil, nil
	}
	events, err := st.HumanEvents(ctx, store.HumanEventsFilter{Since: now.AddDate(0, 0, -days)})
	if err != nil {
		return nil, err
	}
	in := make([]worktime.Event, len(events))
	keys, dirs := []string{}, []string{}
	seen := map[string]bool{}
	noProject := map[string]bool{}
	last := map[string]time.Time{}
	for i, ev := range events {
		in[i] = worktime.Event{Time: ev.Time, Session: ev.SessionID, Project: ev.Project, Dir: ev.Dir, Remote: ev.RemoteURL}
		if seen[ev.Project] {
			if t := last[res.Key(ev.Project, ev.Dir, ev.RemoteURL)]; ev.Time.After(t) {
				last[res.Key(ev.Project, ev.Dir, ev.RemoteURL)] = ev.Time
			}
			continue
		}
		seen[ev.Project] = true
		resolved := res.Key(ev.Project, ev.Dir, ev.RemoteURL)
		if _, ok := res.Definition(resolved); !ok {
			noProject[resolved] = true
		}
		keys = append(keys, resolved)
		last[resolved] = ev.Time
		if ev.Dir && resolved == ev.Project {
			dirs = append(dirs, resolved)
		}
	}
	names := res.Names(keys, dirs, cfg.Home)
	hints := map[string]*setupHint{}
	get := func(key string) *setupHint {
		h := hints[key]
		if h == nil {
			h = &setupHint{name: names[key], key: key, last: last[key]}
			hints[key] = h
		}
		return h
	}
	if checkProjects {
		for key := range noProject {
			get(key).noProject = true
		}
	}
	if checkCategories {
		rep := worktime.Compute(in, settings, worktime.Options{Location: cfg.Location(), DayStart: cfg.DayStartOffset(), Home: cfg.Home, Projects: res})
		for _, u := range rep.Uncategorized {
			get(u.Key).noCategory = true
		}
	}
	out := make([]setupHint, 0, len(hints))
	for _, h := range hints {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// writeSetupHints prints the hints, one line per project.
func writeSetupHints(w io.Writer, hints []setupHint, days int) {
	_, _ = fmt.Fprintf(w, "hint: %s with activity in the last %d days not fully set up in the config:\n", plural(len(hints), "project"), days)
	for _, h := range hints {
		var what []string
		if h.noProject {
			what = append(what, "no [[project]]")
		}
		if h.noCategory {
			what = append(what, "no category")
		}
		_, _ = fmt.Fprintf(w, "  %s: %s\n", h.name, strings.Join(what, ", "))
	}
	_, _ = fmt.Fprintln(w)
}
