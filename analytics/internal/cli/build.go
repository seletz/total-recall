package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/star"
)

// BuildJSON is the output of `build --json`.
type BuildJSON struct {
	AnalyticsDB string `json:"analytics_db"`
	Source      string `json:"source"`
	Host        string `json:"host"`
	Archive     string `json:"archive"`
	TZ          string `json:"tz"`
	BatchID     int64  `json:"batch_id"`
	Mode        string `json:"mode"`
	SeqFrom     int64  `json:"seq_from"`
	SeqTo       int64  `json:"seq_to"`
	Events      int    `json:"events"`
	Sessions    int    `json:"sessions"`
	Models      int    `json:"models"`
	Tests       int    `json:"tests"`
	ElapsedMS   int64  `json:"elapsed_ms"`
}

func newBuildCmd(a *App) *cobra.Command {
	var asJSON, full bool
	var host, source string
	var zones []string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build or update the analytics database from an archive",
		Long: `Loads the archive (the db of the total-recall config or --archive) into the
analytics database: only the sessions with events above the watermark of the
source, or everything with --full. One transaction; the contract of the views
and the data tests must pass, otherwise nothing is written.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := a.config()
			if err != nil {
				return err
			}
			loc, err := location(cfg)
			if err != nil {
				return err
			}
			var extra []*time.Location
			for _, z := range zones {
				l, err := time.LoadLocation(z)
				if err != nil {
					return usagef("--extra-tz %s: %v", z, err)
				}
				extra = append(extra, l)
			}
			if host, err = a.hostName(host); err != nil {
				return err
			}
			if source == "" {
				source = host
			}
			archive, err := filepath.Abs(cfg.DB)
			if err != nil {
				return err
			}
			if _, err := os.Stat(archive); err != nil {
				return errKind(ErrKindDB, fmt.Errorf("archive: %w", err))
			}
			projects, err := cfg.ProjectResolver()
			if err != nil {
				return errKind(ErrKindConfig, err)
			}
			accounts, _, err := cfg.AccountResolver()
			if err != nil {
				return errKind(ErrKindConfig, err)
			}
			ws, err := cfg.WorktimeSettings()
			if err != nil {
				return errKind(ErrKindConfig, err)
			}
			db, path, err := a.openAnalytics(ctx, cfg, false)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			r, err := star.Run(ctx, db, star.Options{
				Archive: archive, Source: source, Host: host, TZ: loc, ExtraZones: extra,
				Home: cfg.Home, Projects: projects, Full: full, Now: a.Now,
				Worktime: &star.Worktime{Settings: ws, DayStart: cfg.DayStartOffset()},
				Accounts: func(id, org string) (string, string) {
					i := accounts.Resolve(id, org)
					return i.Name, i.Group
				},
			})
			if err != nil {
				return errKind(ErrKindDB, err)
			}
			out := BuildJSON{AnalyticsDB: path, Source: r.Source, Host: host, Archive: archive, TZ: loc.String(),
				BatchID: r.BatchID, Mode: r.Mode, SeqFrom: r.SeqFrom, SeqTo: r.SeqTo, Events: r.Events, Sessions: r.Sessions,
				Models: r.Models, Tests: r.Tests, ElapsedMS: r.Elapsed.Milliseconds()}
			if asJSON {
				return writeJSON(a.Stdout, out)
			}
			_, err = fmt.Fprintf(a.Stdout, "%s: batch %d (%s) from %s (host %s), seq %d..%d: %d events in %d sessions, %d models, %d tests passed, %s\n",
				out.AnalyticsDB, out.BatchID, out.Mode, out.Source, out.Host, out.SeqFrom, out.SeqTo, out.Events, out.Sessions,
				out.Models, out.Tests, r.Elapsed.Round(time.Millisecond))
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&asJSON, "json", false, "output as JSON")
	f.BoolVar(&full, "full", false, "reload every event of the source instead of the new ones")
	f.StringVar(&host, "host", "", "host the archive belongs to (default: this host's short name)")
	f.StringVar(&source, "source", "", "name of the source for the watermark (default: the host)")
	f.StringSliceVar(&zones, "extra-tz", nil, "further time zones for analytics_time_slot_v1 (repeatable)")
	return cmd
}

// noArgs rejects positional arguments as a usage error.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usagef("%s takes no arguments, got %q", cmd.Name(), args[0])
	}
	return nil
}
