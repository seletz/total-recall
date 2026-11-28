package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/analytics/internal/star"
)

// StatusJSON is the output of `status --json`.
type StatusJSON struct {
	AnalyticsDB string       `json:"analytics_db"`
	SizeBytes   int64        `json:"size_bytes"`
	TZ          string       `json:"tz"`
	Zones       []string     `json:"zones"`
	Views       int          `json:"views"`
	Sources     []SourceJSON `json:"sources"`
}

// SourceJSON is one loaded archive.
type SourceJSON struct {
	Source       string    `json:"source"`
	Host         string    `json:"host"`
	Archive      string    `json:"archive"`
	ArchiveFound bool      `json:"archive_found"`
	LastSeq      int64     `json:"last_seq"`
	Backlog      int64     `json:"backlog"`
	Events       int64     `json:"events"`
	Sessions     int64     `json:"sessions"`
	LastBatch    BatchJSON `json:"last_batch"`
}

// BatchJSON is one run.
type BatchJSON struct {
	BatchID    int64     `json:"batch_id"`
	Mode       string    `json:"mode"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	SeqFrom    int64     `json:"seq_from"`
	SeqTo      int64     `json:"seq_to"`
	Events     int64     `json:"events"`
	Sessions   int64     `json:"sessions"`
}

func newStatusCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the loaded sources, their watermarks and backlog",
		Long: `Lists every source (archive) of the analytics database: host, watermark,
loaded events and sessions, the last batch, and the backlog: events of the
archive above the watermark that the next build would load.`,
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
			srcs, err := star.Sources(ctx, db)
			if err != nil {
				return errKind(ErrKindDB, err)
			}
			out := StatusJSON{AnalyticsDB: path, Views: len(star.Views()), Zones: []string{}, Sources: []SourceJSON{}}
			if fi, err := os.Stat(path); err == nil {
				out.SizeBytes = fi.Size()
			}
			if out.TZ, err = star.Setting(ctx, db, "tz"); err != nil {
				return errKind(ErrKindDB, err)
			}
			if z, err := star.Setting(ctx, db, "zones"); err != nil {
				return errKind(ErrKindDB, err)
			} else if z != "" {
				out.Zones = strings.Split(z, ",")
			}
			for _, s := range srcs {
				j := SourceJSON{Source: s.Name, Host: s.Host, Archive: s.Archive, LastSeq: s.LastSeq, Events: s.Events, Sessions: s.Sessions,
					LastBatch: BatchJSON{BatchID: s.Batch.ID, Mode: s.Batch.Mode, StartedAt: s.Batch.StartedAt, FinishedAt: s.Batch.FinishedAt,
						SeqFrom: s.Batch.SeqFrom, SeqTo: s.Batch.SeqTo, Events: s.Batch.Events, Sessions: s.Batch.Sessions}}
				if _, err := os.Stat(s.Archive); err == nil {
					j.ArchiveFound = true
					if j.Backlog, err = backlog(ctx, db, s.Archive, s.LastSeq); err != nil {
						return errKind(ErrKindDB, err)
					}
				}
				out.Sources = append(out.Sources, j)
			}
			if asJSON {
				return writeJSON(a.Stdout, out)
			}
			_, _ = fmt.Fprintf(a.Stdout, "%s: %s, %d views, zone %s\n\n", out.AnalyticsDB, humanBytes(out.SizeBytes), out.Views, strings.Join(out.Zones, ", "))
			tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "SOURCE\tHOST\tEVENTS\tSESSIONS\tLAST SEQ\tBACKLOG\tLAST BATCH\tARCHIVE")
			for _, s := range out.Sources {
				bl := fmt.Sprint(s.Backlog)
				if !s.ArchiveFound {
					bl = "?"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t%d %s %s\t%s\n", s.Source, s.Host, s.Events, s.Sessions, s.LastSeq, bl,
					s.LastBatch.BatchID, s.LastBatch.Mode, s.LastBatch.FinishedAt.Format(time.RFC3339), s.Archive)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

// backlog counts the archive's events above the watermark.
func backlog(ctx context.Context, db *duck.DB, archive string, last int64) (int64, error) {
	if err := db.AttachSQLite(ctx, archive, "backlog_src"); err != nil {
		return 0, err
	}
	defer func() { _, _ = db.ExecContext(context.WithoutCancel(ctx), "DETACH backlog_src") }()
	var n int64
	err := db.QueryRowContext(ctx, "SELECT count(*) FROM backlog_src.events WHERE seq > ?", last).Scan(&n)
	return n, err
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
