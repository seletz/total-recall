package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/analytics/internal/star"
	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/pool"
)

// BuildJSON is the output of `build --json`, and one entry of
// `build --pool --json`.
type BuildJSON struct {
	AnalyticsDB string `json:"analytics_db"`
	Source      string `json:"source"`
	Host        string `json:"host"`
	Archive     string `json:"archive"`
	// ArchiveID is the archive's own ID (total-recall schema v11), null
	// before; Replaced the sources that had loaded the same archive under
	// another name and were removed (a renamed host).
	ArchiveID *string  `json:"archive_id"`
	Replaced  []string `json:"replaced"`
	TZ        string   `json:"tz"`
	BatchID   int64    `json:"batch_id"`
	Mode      string   `json:"mode"`
	SeqFrom   int64    `json:"seq_from"`
	SeqTo     int64    `json:"seq_to"`
	Events    int      `json:"events"`
	Sessions  int      `json:"sessions"`
	Models    int      `json:"models"`
	Tests     int      `json:"tests"`
	ElapsedMS int64    `json:"elapsed_ms"`
}

// BuildPoolJSON is the output of `build --pool --json`.
type BuildPoolJSON struct {
	AnalyticsDB string `json:"analytics_db"`
	Pool        string `json:"pool"`
	Cache       string `json:"cache"`
	// Builds are the archives loaded, by host; Skipped the copies of the
	// pool that do not count, with the reason (pool.Select).
	Builds  []BuildJSON   `json:"builds"`
	Skipped []SkippedJSON `json:"skipped"`
}

// SkippedJSON is a copy in the pool that was not loaded.
type SkippedJSON struct {
	Host   string `json:"host"`
	Backup string `json:"backup"`
	Reason string `json:"reason"`
}

// buildRun is what every load of an archive shares.
type buildRun struct {
	a     *App
	cfg   *config.Config
	db    *duck.DB
	path  string
	opts  star.Options
	asOne bool
}

func newBuildCmd(a *App) *cobra.Command {
	var asJSON, full, fromPool bool
	var host, source, poolDir, cache string
	var zones []string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build or update the analytics database from an archive or the pool",
		Long: `Loads the archive (the db of the total-recall config or --archive) into the
analytics database: only the sessions with events above the watermark of the
source, or everything with --full. One transaction; the contract of the views
and the data tests must pass, otherwise nothing is written.

With --pool the archives come from the pool directory ([backup] pool_dir of
the config, or --pool-dir): the newest backup of every host is unpacked into
the cache and loaded as its own source, under the host its archive names.
Every archive counts once: copies of the same archive and two archives that
claim the same host are skipped with the reason.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if poolDir != "" {
				fromPool = true
			}
			if fromPool && (host != "" || source != "") {
				return usagef("--pool loads every host of the pool under the host its archive names: --host and --source name one archive")
			}
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
			if fromPool {
				if poolDir == "" {
					poolDir = cfg.Backup.PoolDir
				}
				if poolDir == "" {
					return usagef("no pool directory: set [backup] pool_dir in the config or pass --pool-dir")
				}
				if cache == "" {
					uc, err := os.UserCacheDir()
					if err != nil {
						return err
					}
					cache = filepath.Join(uc, "total-recall", "pool")
				}
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
			b := buildRun{a: a, cfg: cfg, db: db, path: path, asOne: !asJSON, opts: star.Options{
				TZ: loc, ExtraZones: extra, Home: cfg.Home, Projects: projects, Full: full, Now: a.Now,
				Worktime: &star.Worktime{Settings: ws, DayStart: cfg.DayStartOffset()},
				Accounts: func(id, org string) (string, string) {
					i := accounts.Resolve(id, org)
					return i.Name, i.Group
				},
			}}
			if fromPool {
				return b.pool(ctx, poolDir, cache, asJSON)
			}
			archive, err := filepath.Abs(cfg.DB)
			if err != nil {
				return err
			}
			if _, err := os.Stat(archive); err != nil {
				return errKind(ErrKindDB, fmt.Errorf("archive: %w", err))
			}
			id, _, err := pool.Identity(ctx, archive)
			if err != nil {
				return errKind(ErrKindDB, fmt.Errorf("archive: %w", err))
			}
			// The host: --host, else what the archive recorded, else this
			// machine's short name.
			if host == "" {
				host = id.Host
			}
			if host, err = a.hostName(host); err != nil {
				return err
			}
			if source == "" {
				source = host
			}
			out, err := b.load(ctx, archive, source, host, id.ID)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(a.Stdout, out)
			}
			return writeBuildLine(a.Stdout, out, "")
		},
	}
	f := cmd.Flags()
	f.BoolVar(&asJSON, "json", false, "output as JSON")
	f.BoolVar(&full, "full", false, "reload every event of the source instead of the new ones")
	f.StringVar(&host, "host", "", "host the archive belongs to (default: the host the archive recorded, else this host's short name)")
	f.StringVar(&source, "source", "", "name of the source for the watermark (default: the host)")
	f.StringSliceVar(&zones, "extra-tz", nil, "further time zones for analytics_time_slot_v1 (repeatable)")
	f.BoolVar(&fromPool, "pool", false, "load every host of the pool directory ([backup] pool_dir) instead of one archive")
	f.StringVar(&poolDir, "pool-dir", "", "pool directory for --pool (implies --pool)")
	f.StringVar(&cache, "cache", "", "where --pool unpacks the backups (default <user cache dir>/total-recall/pool)")
	return cmd
}

// load runs the ETL for one archive.
func (b buildRun) load(ctx context.Context, archive, source, host, archiveID string) (BuildJSON, error) {
	o := b.opts
	o.Archive, o.Source, o.Host, o.ArchiveID = archive, source, host, archiveID
	r, err := star.Run(ctx, b.db, o)
	if err != nil {
		return BuildJSON{}, errKind(ErrKindDB, fmt.Errorf("%s: %w", source, err))
	}
	out := BuildJSON{AnalyticsDB: b.path, Source: r.Source, Host: host, Archive: archive, Replaced: append([]string{}, r.Replaced...),
		TZ: o.TZ.String(), BatchID: r.BatchID, Mode: r.Mode, SeqFrom: r.SeqFrom, SeqTo: r.SeqTo, Events: r.Events, Sessions: r.Sessions,
		Models: r.Models, Tests: r.Tests, ElapsedMS: r.Elapsed.Milliseconds()}
	if archiveID != "" {
		out.ArchiveID = &archiveID
	}
	return out, nil
}

// pool loads every archive of the pool directory that counts.
func (b buildRun) pool(ctx context.Context, dir, cache string, asJSON bool) error {
	entries, err := pool.Scan(dir, b.opts.TZ)
	if err != nil {
		return errKind(ErrKindDB, err)
	}
	var opened []pool.Archive
	for _, e := range entries {
		a, err := pool.Open(ctx, e, cache)
		if err != nil {
			return errKind(ErrKindDB, err)
		}
		opened = append(opened, a)
	}
	keep, skipped := pool.Select(opened)
	out := BuildPoolJSON{AnalyticsDB: b.path, Pool: dir, Cache: cache, Builds: []BuildJSON{}, Skipped: []SkippedJSON{}}
	for _, s := range skipped {
		out.Skipped = append(out.Skipped, SkippedJSON(s))
	}
	if !asJSON {
		_, _ = fmt.Fprintf(b.a.Stdout, "%s: pool %s, %d archives\n", b.path, dir, len(keep))
	}
	for _, a := range keep {
		r, err := b.load(ctx, a.Path, a.HostName(), a.HostName(), a.Archive.ID)
		if err != nil {
			return err
		}
		out.Builds = append(out.Builds, r)
		if !asJSON {
			if err := writeBuildLine(b.a.Stdout, r, "  "); err != nil {
				return err
			}
		}
	}
	if asJSON {
		return writeJSON(b.a.Stdout, out)
	}
	for _, s := range out.Skipped {
		if _, err := fmt.Fprintf(b.a.Stdout, "skipped %s: %s\n", s.Host, s.Reason); err != nil {
			return err
		}
	}
	return nil
}

// writeBuildLine prints the line of one load; the pool indents it and
// leaves out the database.
func writeBuildLine(w io.Writer, out BuildJSON, indent string) error {
	head := out.AnalyticsDB + ": "
	if indent != "" {
		head = indent
	}
	var err error
	if _, err = fmt.Fprintf(w, "%sbatch %d (%s) from %s (host %s), seq %d..%d: %d events in %d sessions, %d models, %d tests passed, %s\n",
		head, out.BatchID, out.Mode, out.Source, out.Host, out.SeqFrom, out.SeqTo, out.Events, out.Sessions,
		out.Models, out.Tests, (time.Duration(out.ElapsedMS) * time.Millisecond).Round(time.Millisecond)); err != nil {
		return err
	}
	for _, s := range out.Replaced {
		if _, err = fmt.Fprintf(w, "%s  replaced source %s: the same archive under its old name\n", indent, s); err != nil {
			return err
		}
	}
	return nil
}

// noArgs rejects positional arguments as a usage error.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usagef("%s takes no arguments, got %q", cmd.Name(), args[0])
	}
	return nil
}
