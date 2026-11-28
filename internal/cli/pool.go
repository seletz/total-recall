package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/pool"
)

// PoolJSON is the output of `pool --json`.
type PoolJSON struct {
	Pool  string         `json:"pool"`
	Cache string         `json:"cache"`
	Hosts []PoolHostJSON `json:"hosts"`
}

// PoolHostJSON is one host folder of the pool with its newest copy.
type PoolHostJSON struct {
	Host        string     `json:"host"`
	Backup      string     `json:"backup"`
	BackupAt    string     `json:"backup_at"`
	BackupBytes int64      `json:"backup_bytes"`
	Cached      string     `json:"cached"`
	ArchiveHost NullString `json:"archive_host"`
	ArchiveID   NullString `json:"archive_id"`
	// SchemaVersion is the schema of the archive; Sessions and Events
	// count it.
	SchemaVersion int `json:"schema_version"`
	Sessions      int `json:"sessions"`
	Events        int `json:"events"`
	// Problems name what keeps the copy from counting as this host's
	// archive; empty when it fits.
	Problems []string `json:"problems"`
}

func (e *env) poolCmd() *cobra.Command {
	var of outputFlags
	var dir, cache string
	c := &cobra.Command{
		Use:   "pool",
		Short: "List the newest backup of every host in the pool directory and check it",
		Long: `List the pool directory ([backup] pool_dir, or --dir): one folder per
host with its newest backup. Each copy is unpacked into the cache (once per
copy) and read without writing, to show what the archive says about itself
(host, ID, schema) and how much it holds. A copy whose archive names
another host, or the same archive in two folders, is listed with the
problem. Reads only; the pool is never written.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.Flags().StringVar(&dir, "dir", "", "pool directory (default [backup] pool_dir)")
	c.Flags().StringVar(&cache, "cache", "", "where the copies are unpacked (default <user cache dir>/total-recall/pool)")
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		if dir == "" {
			dir = cfg.Backup.PoolDir
		}
		if dir == "" {
			return usagef("no pool directory: set [backup] pool_dir in the config or pass --dir")
		}
		if cache == "" {
			uc, err := os.UserCacheDir()
			if err != nil {
				return err
			}
			cache = filepath.Join(uc, "total-recall", "pool")
		}
		entries, err := pool.Scan(dir, cfg.Location())
		if err != nil {
			return err
		}
		out := PoolJSON{Pool: dir, Cache: cache, Hosts: []PoolHostJSON{}}
		byID := map[string][]string{}
		for _, en := range entries {
			a, err := pool.Open(cmd.Context(), en, cache)
			if err != nil {
				return err
			}
			h := PoolHostJSON{Host: en.Host, Backup: en.Path, BackupAt: isoTime(en.At), BackupBytes: en.Size, Cached: a.Path,
				ArchiveHost: NullString(a.Archive.Host), ArchiveID: NullString(a.Archive.ID), SchemaVersion: a.SchemaVersion, Problems: []string{}}
			if h.Sessions, h.Events, err = pool.Counts(cmd.Context(), a); err != nil {
				return err
			}
			switch {
			case a.Archive.ID == "":
				h.Problems = append(h.Problems, fmt.Sprintf("schema v%d has no host yet: run migrate on %s", a.SchemaVersion, en.Host))
			case a.Archive.Host != en.Host:
				h.Problems = append(h.Problems, fmt.Sprintf("archive names host %s", cmpOr(a.Archive.Host, "none")))
			}
			if a.Archive.ID != "" {
				byID[a.Archive.ID] = append(byID[a.Archive.ID], en.Host)
			}
			out.Hosts = append(out.Hosts, h)
		}
		for i, h := range out.Hosts {
			for _, other := range byID[string(h.ArchiveID)] {
				if other != h.Host {
					out.Hosts[i].Problems = append(out.Hosts[i].Problems, "same archive as "+other)
				}
			}
		}
		if o.format.IsJSON() {
			return o.object(out)
		}
		now := e.app.Now()
		rows := make([][]string, 0, len(out.Hosts))
		for i, h := range out.Hosts {
			note := strings.Join(h.Problems, "; ")
			if note == "" {
				note = "ok"
			}
			rows = append(rows, []string{h.Host, filepath.Base(h.Backup), age(now, entries[i].At), sizeLabel(h.BackupBytes),
				cmpOr(string(h.ArchiveHost), "-"), cmpOr(string(h.ArchiveID), "-"), "v" + strconv.Itoa(h.SchemaVersion),
				strconv.Itoa(h.Sessions), strconv.Itoa(h.Events), note})
		}
		if len(rows) == 0 {
			_, _ = fmt.Fprintf(o.w, "no backups in %s\n", dir)
			return nil
		}
		return o.table([]string{"HOST", "BACKUP", "AGE", "SIZE", "ARCHIVE HOST", "ARCHIVE", "SCHEMA", "SESSIONS", "EVENTS", "NOTE"}, rows)
	})
	of.register(c)
	return c
}

func cmpOr(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}
