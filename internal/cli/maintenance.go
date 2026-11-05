package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/store"
)

// RecommendedPageSize is the page size maintenance uses by default and
// status recommends; see ops.RecommendedPageSize.
const RecommendedPageSize = ops.RecommendedPageSize

// MaintenanceJSON is the JSON output of maintenance. Every key is always
// present; Backup is null without a backup (dry run).
type MaintenanceJSON struct {
	DB         string          `json:"db"`
	DryRun     bool            `json:"dry_run"`
	Swapped    bool            `json:"swapped"`
	PageSize   int             `json:"page_size"`
	Backup     NullString      `json:"backup"`
	Before     store.PageStats `json:"before"`
	After      store.PageStats `json:"after"`
	SavedBytes int64           `json:"saved_bytes"`
	DurationMS int64           `json:"duration_ms"`
}

func (e *env) maintenanceCmd() *cobra.Command {
	var of outputFlags
	var pageSize int
	var dryRun bool
	var wait time.Duration
	c := &cobra.Command{
		Use:   "maintenance",
		Short: "Rebuild the archive with a new page size (VACUUM) after a backup",
		Long: `Rebuild the archive file compactly with the given page size and replace it
atomically: backup as with "backup", exclusive lock against hooks, WAL
checkpoint, PRAGMA page_size + VACUUM INTO a copy next to the database,
integrity check and row counts of the copy, then the swap. Any failure
before the swap leaves the archive untouched.

Hooks that start while the lock is held skip their run and catch up from
the stored offset at the next hook. Readers (TUI, status) block the
checkpoint; close them first. Needs free space of about twice the archive
size next to the database.

  --dry-run                build and measure the copy only, then delete it:
                           no backup, no lock, the archive is not changed`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 {
			return usagef("--page-size %d is not a power of two between 512 and 65536", pageSize)
		}
		if _, err := os.Stat(cfg.DB); err != nil {
			return errKind(ErrKindDB, fmt.Errorf("no archive at %s", cfg.DB))
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		opts := ops.MaintainOptions(cfg, e.app.Now(), pageSize, dryRun, wait)
		log := logfile.New(cfg.LogPath(), e.app.Now)
		opts.Logf = func(f string, a ...any) {
			log.Infof(f, a...)
			if !o.format.IsJSON() {
				_, _ = fmt.Fprintf(o.w, f+"\n", a...)
			}
		}
		res, err := store.Maintain(ctx, cfg.DB, opts)
		if err != nil {
			return maintenanceError(err)
		}
		out := MaintenanceJSON{DB: cfg.DB, DryRun: res.DryRun, Swapped: res.Swapped, PageSize: pageSize,
			Backup: NullString(res.Backup), Before: res.Before, After: res.After,
			SavedBytes: res.Before.SizeBytes - res.After.SizeBytes, DurationMS: res.Duration.Milliseconds()}
		if o.format.IsJSON() {
			return o.object(out)
		}
		if res.DryRun {
			_, _ = fmt.Fprintln(o.w, "dry run: the archive was not changed; the rebuilt copy was measured and deleted")
		}
		return o.table([]string{"", "BEFORE", "AFTER"}, maintenanceRows(res.Before, res.After))
	})
	of.register(c)
	c.Flags().IntVar(&pageSize, "page-size", RecommendedPageSize, "page size of the rebuilt archive (power of two, 512 to 65536)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "measure only: build and delete the copy, change nothing")
	c.Flags().DurationVar(&wait, "wait", 60*time.Second, "how long to wait for running hooks to release the archive")
	return c
}

// maintenanceError tags a Maintain error with its kind.
func maintenanceError(err error) error {
	switch {
	case errors.Is(err, store.ErrWritersActive), errors.Is(err, store.ErrReadersActive), errors.Is(err, store.ErrDiskSpace),
		errors.Is(err, store.ErrMaintenance):
		return errKind(ErrKindDB, err)
	}
	return err
}

// maintenanceRows renders the before/after comparison.
func maintenanceRows(b, a store.PageStats) [][]string {
	size := func(ps store.PageStats) string { return fmt.Sprintf("%.1f MiB", float64(ps.SizeBytes)/(1<<20)) }
	after := size(a)
	if b.SizeBytes > 0 {
		after += fmt.Sprintf("  (%+.1f %%)", float64(a.SizeBytes-b.SizeBytes)*100/float64(b.SizeBytes))
	}
	n := func(v int64) string { return fmt.Sprint(v) }
	return [][]string{
		{"size", size(b), after},
		{"page size", n(int64(b.PageSize)), n(int64(a.PageSize))},
		{"pages", n(b.PageCount), n(a.PageCount)},
		{"freelist pages", n(b.FreelistPages), n(a.FreelistPages)},
		{"sessions", n(int64(b.Sessions)), n(int64(a.Sessions))},
		{"events", n(int64(b.Events)), n(int64(a.Events))},
		{"sources", n(int64(b.Sources)), n(int64(a.Sources))},
	}
}
