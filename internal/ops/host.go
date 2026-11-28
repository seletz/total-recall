package ops

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/pool"
	"github.com/seletz/total-recall/internal/store"
)

// HostCell describes the host of the archive for status and the status
// page of the TUI: the configured host, then a note on what the archive
// recorded. The note is a warning (warn) when the archive does not name
// the configured host yet.
func HostCell(host string, a store.Archive) (value, note string, warn bool) {
	value = host
	if value == "" {
		value = "unknown"
	}
	switch {
	case a.ID == "":
		return value, "the archive records it after migrate", true
	case host == "" && a.Host == "":
		return value, "archive " + a.ID + ", no host: set host in the config", true
	case host == "":
		return a.Host, "archive " + a.ID + ", host unknown on this machine: set host in the config", true
	case a.Host == "":
		return value, "archive " + a.ID + ", recorded at the next write", true
	case a.Host != host:
		return value, "archive " + a.ID + ", recorded as " + a.Host + " until the next write", true
	}
	return value, "archive " + a.ID, false
}

// PoolCopy is this host's copy in the pool directory of cfg ([backup]
// pool_dir): Dir is <pool_dir>/<host>, Path the newest copy there and At
// when it was made; Path is empty without a copy, Dir also without a valid
// host. A zero PoolCopy (Configured false) means no pool directory.
type PoolCopy struct {
	Configured bool
	Dir, Path  string
	At         time.Time
}

// ReadPoolCopy looks up this host's copy in the pool directory of cfg.
func ReadPoolCopy(cfg *config.Config) (PoolCopy, error) {
	if cfg.Backup.PoolDir == "" {
		return PoolCopy{}, nil
	}
	pc := PoolCopy{Configured: true}
	if !config.ValidHost(cfg.Host) {
		return pc, nil
	}
	pc.Dir = filepath.Join(cfg.Backup.PoolDir, cfg.Host)
	var err error
	pc.Path, pc.At, err = store.LastBackup(pc.Dir, cfg.Location())
	return pc, err
}

// PoolCell describes the pool line of status and of the status page of
// the TUI: this host's copy, or why there is none (warn). age formats the
// age of the copy.
func PoolCell(cfg *config.Config, pc PoolCopy, age func(time.Time) string) (value string, warn bool) {
	switch {
	case pc.Dir == "":
		return cfg.Backup.PoolDir + "  host unknown: set host in the config", true
	case pc.Path == "":
		return pc.Dir + "  no copy yet: the next backup writes one", true
	}
	return pc.Path + "  " + age(pc.At), false
}

// PublishPool puts the newest backup of cfg into its pool directory
// (pool.Publish) when [backup] pool_dir is set; ok is false without one.
// `backup` and the backup action of the TUI call it after every backup.
func PublishPool(cfg *config.Config) (r pool.Published, ok bool, err error) {
	if cfg.Backup.PoolDir == "" {
		return r, false, nil
	}
	r, err = pool.Publish(cfg.Backup.Dir, cfg.Backup.PoolDir, cfg.Host, cfg.Location())
	if err != nil {
		return r, true, fmt.Errorf("pool copy: %w", err)
	}
	return r, true, nil
}
