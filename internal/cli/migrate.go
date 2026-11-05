package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/store"
)

// MigrateJSON is the JSON output of migrate. Every key is always present;
// Backup is null when nothing is pending.
type MigrateJSON struct {
	DB         string                `json:"db"`
	DryRun     bool                  `json:"dry_run"`
	From       int                   `json:"from"`
	To         int                   `json:"to"`
	Pending    bool                  `json:"pending"`
	Steps      []store.MigrationInfo `json:"steps"`
	SizeBytes  int64                 `json:"size_bytes"`
	NeedBytes  int64                 `json:"need_bytes"`
	Backup     NullString            `json:"backup"`
	Migrated   bool                  `json:"migrated"`
	DurationMS int64                 `json:"duration_ms"`
}

// progressInterval is how often migrate prints the progress of a running
// migration; the final count is always printed.
const progressInterval = 2 * time.Second

// migrateHint is the advice every message about an outdated archive ends with.
const migrateHint = "run `total-recall backup`, then `total-recall migrate`"

func (e *env) migrateCmd() *cobra.Command {
	var of outputFlags
	var dryRun, yes bool
	var wait time.Duration
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Bring the archive to the schema of this version (after a backup)",
		Long: `Show the schema version of the archive and of this binary with every pending
migration, ask for confirmation and migrate. Nothing else migrates: a hook
or ingest that meets an older archive writes nothing until this ran.

Before: "total-recall backup". The command then checks the free space,
takes the exclusive lock of "maintenance" (hooks that start meanwhile skip
their run and catch up afterwards), writes a copy <db>.pre-v<N>.bak next to
the database and applies the migrations one by one, each in its own
transaction. A failure leaves the archive at the last version that
completed; the copy stays.

  --dry-run                show the plan only, change nothing
  --yes                    do not ask (scripts)`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		if _, err := os.Stat(cfg.DB); err != nil {
			return errKind(ErrKindDB, fmt.Errorf("no archive at %s", cfg.DB))
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		plan, err := store.PlanMigration(ctx, cfg.DB)
		if err != nil {
			return errKind(ErrKindDB, err)
		}
		out := migrateJSON(plan, dryRun)
		text := !o.format.IsJSON()
		if text {
			printPlan(o, plan)
		}
		if !plan.Pending() || dryRun {
			if text && plan.Pending() {
				_, _ = fmt.Fprintln(o.w, "dry run: nothing changed")
			}
			if o.format.IsJSON() {
				return o.object(out)
			}
			return nil
		}
		if !yes {
			if !confirm(cmd, "Migrate now? Hooks that run meanwhile skip and catch up afterwards. [y/N] ") {
				return errors.New("migration cancelled, the archive is unchanged")
			}
		}
		opts := store.MigrateOptions{LockDir: lockDir(cfg), Wait: wait, RawTrimmers: rawTrimmers(adapters(cfg))}
		if text {
			opts.Logf = func(f string, a ...any) { _, _ = fmt.Fprintf(o.w, f+"\n", a...) }
			opts.Progress = progressPrinter(o, e.app.Now)
		}
		res, err := store.Migrate(ctx, cfg.DB, opts)
		if err != nil {
			return maintenanceError(err)
		}
		out.Migrated, out.DurationMS = true, res.Duration.Milliseconds()
		if o.format.IsJSON() {
			return o.object(out)
		}
		_, _ = fmt.Fprintf(o.w, "migrated v%d -> v%d (%s); the copy %s can be removed once everything works\n",
			plan.From, plan.To, res.Duration.Round(time.Millisecond), plan.Backup)
		return nil
	})
	of.register(c)
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show the pending migrations only, change nothing")
	c.Flags().BoolVar(&yes, "yes", false, "migrate without asking")
	c.Flags().DurationVar(&wait, "wait", 60*time.Second, "how long to wait for running hooks to release the archive")
	return c
}

// migrateJSON builds the output object from a plan.
func migrateJSON(plan store.MigratePlan, dryRun bool) MigrateJSON {
	return MigrateJSON{DB: plan.DB, DryRun: dryRun, From: plan.From, To: plan.To, Pending: plan.Pending(), Steps: plan.Steps,
		SizeBytes: plan.SizeBytes, NeedBytes: plan.NeedBytes, Backup: NullString(plan.Backup)}
}

// printPlan renders the plan as text.
func printPlan(o *output, plan store.MigratePlan) {
	p := func(f string, a ...any) { _, _ = fmt.Fprintf(o.w, f, a...) }
	p("database  %s  (%.1f MiB)\n", plan.DB, float64(plan.SizeBytes)/(1<<20))
	if !plan.Pending() {
		p("schema    v%d is current, nothing to migrate\n", plan.From)
		return
	}
	noun := "migrations"
	if len(plan.Steps) == 1 {
		noun = "migration"
	}
	p("schema    v%d -> v%d, %d pending %s:\n", plan.From, plan.To, len(plan.Steps), noun)
	for _, s := range plan.Steps {
		p("  v%-4d %s\n", s.Version, s.Summary)
	}
	p("backup    %s\n", plan.Backup)
	p("space     about %.1f MiB free needed next to the database (copy and migration)\n", float64(plan.NeedBytes)/(1<<20))
}

// confirm asks the question on the command's output and reads one line from
// its input; only "y" or "yes" (any case) is a yes.
func confirm(cmd *cobra.Command, question string) bool {
	_, _ = fmt.Fprint(cmd.OutOrStdout(), question)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// progressPrinter prints the progress of a migration, at most every
// progressInterval and when it completes.
func progressPrinter(o *output, now func() time.Time) func(store.Progress) {
	var last time.Time
	return func(p store.Progress) {
		if p.Total == 0 {
			return
		}
		t := now()
		if p.Done < p.Total && t.Sub(last) < progressInterval {
			return
		}
		last = t
		_, _ = fmt.Fprintf(o.w, "migration v%d: %d/%d (%d %%)\n", p.Version, p.Done, p.Total, p.Done*100/p.Total)
	}
}
