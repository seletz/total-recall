// Package pool is the pool directory of total-recall: the place where the
// hosts of a pool put their backups, so a reader (the analytics tool) finds
// every host's archive in one place without a service.
//
// Layout: one folder per host, named like the config key host, holding
// that host's newest backup under its usual name:
//
//	<pool>/<host>/archive-YYYY-MM-DD[-HHMM[-N]].db.zst
//
// Publish puts a host's newest local backup there (`backup` does it when
// [backup] pool_dir is set); Scan lists the newest copy per host; Open
// unpacks a copy into a cache and reads it with immutable=1, so the pool
// is never written by a reader and a copy needs no -wal or -shm file.
package pool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
)

// Published is what Publish did.
type Published struct {
	// Path is the host's copy in the pool.
	Path string
	// Copied is false when the pool already had this copy.
	Copied bool
	// Removed are the older copies of the host that the new one replaced.
	Removed []string
}

// Publish copies the newest backup in backupDir into <pool>/<host>/ unless
// it is there already, keeping its name and modification time, and then
// removes the host's older copies there, so the pool holds one copy per
// host. The copy is written to a temporary file in the host's folder and
// renamed, so a reader never sees a partial copy under a backup name.
// loc is the time zone of the backup names.
func Publish(backupDir, pool, host string, loc *time.Location) (Published, error) {
	var r Published
	if !config.ValidHost(host) {
		return r, fmt.Errorf("pool: host %q is no valid folder name: set host in the config", host)
	}
	src, _, err := store.LastBackup(backupDir, loc)
	if err != nil {
		return r, fmt.Errorf("pool: %w", err)
	}
	if src == "" {
		return r, fmt.Errorf("pool: no backup in %s to publish", backupDir)
	}
	fi, err := os.Stat(src)
	if err != nil {
		return r, fmt.Errorf("pool: %w", err)
	}
	dir := filepath.Join(pool, host)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return r, fmt.Errorf("pool: %w", err)
	}
	r.Path = filepath.Join(dir, filepath.Base(src))
	if have, err := os.Stat(r.Path); err == nil && have.Size() == fi.Size() {
		return r, removeOthers(dir, filepath.Base(src), &r)
	}
	if err := copyFile(src, r.Path, fi.ModTime()); err != nil {
		return r, fmt.Errorf("pool: %w", err)
	}
	r.Copied = true
	return r, removeOthers(dir, filepath.Base(src), &r)
}

// copyFile copies src to dst through a temporary file next to dst and
// gives it the modification time mtime (a backup named with a date only
// takes its time of day from it).
func copyFile(src, dst string, mtime time.Time) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tf, err := os.CreateTemp(filepath.Dir(dst), "archive-*.tmp")
	if err != nil {
		return err
	}
	tmp := tf.Name()
	defer func() { _ = os.Remove(tmp) }()
	_, err = io.Copy(tf, in)
	if err == nil {
		err = tf.Sync()
	}
	if cerr := tf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	if err := os.Chtimes(tmp, mtime, mtime); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// removeOthers deletes the backup copies in dir other than keep.
func removeOthers(dir, keep string, r *Published) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("pool: %w", err)
	}
	for _, e := range ents {
		if e.IsDir() || e.Name() == keep || !store.IsBackupName(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("pool: %w", err)
		}
		r.Removed = append(r.Removed, p)
	}
	return nil
}

// Entry is a host's newest copy in the pool.
type Entry struct {
	Host string
	Path string
	// At is when the copy was made (see store.LastBackup), Size its size
	// in bytes.
	At   time.Time
	Size int64
}

// Scan returns the newest copy of every host folder in pool, by host
// name. Folders whose name is no valid host (hidden ones, for example)
// and folders without a copy are left out; a missing pool is an error.
func Scan(pool string, loc *time.Location) ([]Entry, error) {
	ents, err := os.ReadDir(pool)
	if err != nil {
		return nil, fmt.Errorf("pool: %w", err)
	}
	var out []Entry
	for _, e := range ents {
		if !e.IsDir() || !config.ValidHost(e.Name()) {
			continue
		}
		p, at, err := store.LastBackup(filepath.Join(pool, e.Name()), loc)
		if err != nil {
			return nil, fmt.Errorf("pool: %w", err)
		}
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("pool: %w", err)
		}
		out = append(out, Entry{Host: e.Name(), Path: p, At: at, Size: fi.Size()})
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Host, b.Host) })
	return out, nil
}

// Archive is a pool copy unpacked into the cache.
type Archive struct {
	Entry
	// Path is the unpacked database in the cache (Entry.Path is the copy
	// in the pool).
	Path string
	// Unpacked is false when the cache already held this copy.
	Unpacked bool
	// Archive is the identity the archive recorded; SchemaVersion its
	// schema.
	Archive       store.Archive
	SchemaVersion int
}

// URI is the SQLite URI to read the cached database: read-only and
// immutable, so SQLite takes no locks and writes no -wal or -shm file.
func (a Archive) URI() string { return ReadURI(a.Path) }

// ReadURI is the SQLite URI that reads the database at path read-only and
// immutable.
func ReadURI(path string) string {
	esc := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	return "file:" + esc + "?mode=ro&immutable=1"
}

// Open unpacks e into <cache>/<host>/ (store.Restore checks the database
// on the way) unless the cache holds it already, removes the host's older
// unpacked copies and reads the identity of the archive. A cached copy is
// known by a stamp file with the name, size and time of the pool copy.
func Open(ctx context.Context, e Entry, cache string) (Archive, error) {
	a := Archive{Entry: e}
	dir := filepath.Join(cache, e.Host)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return a, fmt.Errorf("pool: %w", err)
	}
	base := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(e.Path), ".zst"), ".gz")
	a.Path = filepath.Join(dir, base)
	stamp := fmt.Sprintf("%s %d %d\n", filepath.Base(e.Path), e.Size, e.At.UnixNano())
	if b, err := os.ReadFile(a.Path + ".src"); err != nil || string(b) != stamp {
		if err := store.Restore(ctx, e.Path, a.Path, true); err != nil {
			return a, fmt.Errorf("pool: %s: %w", e.Host, err)
		}
		if err := os.WriteFile(a.Path+".src", []byte(stamp), 0o644); err != nil {
			return a, fmt.Errorf("pool: %w", err)
		}
		a.Unpacked = true
	}
	if err := cleanCache(dir, base); err != nil {
		return a, err
	}
	var err error
	a.Archive, a.SchemaVersion, err = Identity(ctx, a.Path)
	if err != nil {
		return a, fmt.Errorf("pool: %s: %w", e.Host, err)
	}
	return a, nil
}

// cleanCache removes everything in a host's cache folder but the database
// keep and its stamp.
func cleanCache(dir, keep string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("pool: %w", err)
	}
	for _, e := range ents {
		if n := e.Name(); n == keep || n == keep+".src" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("pool: %w", err)
		}
	}
	return nil
}

// Identity reads the schema version and archive_meta of the database at
// path, read-only and immutable (ReadURI); an archive before schema v11
// has none.
func Identity(ctx context.Context, path string) (store.Archive, int, error) {
	var a store.Archive
	db, err := sql.Open("sqlite", ReadURI(path))
	if err != nil {
		return a, 0, err
	}
	defer func() { _ = db.Close() }()
	var v int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v); err != nil {
		return a, 0, fmt.Errorf("schema version: %w", err)
	}
	if v < store.ArchiveVersion {
		return a, v, nil
	}
	rows, err := db.QueryContext(ctx, "SELECT key, value FROM archive_meta WHERE key IN ('archive_id', 'host')")
	if err != nil {
		return a, v, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, val string
		if err := rows.Scan(&k, &val); err != nil {
			return a, v, err
		}
		if k == "archive_id" {
			a.ID = val
		} else {
			a.Host = val
		}
	}
	return a, v, rows.Err()
}

// Counts returns the number of sessions and events of the cached archive.
func Counts(ctx context.Context, a Archive) (sessions, events int, err error) {
	db, err := sql.Open("sqlite", a.URI())
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = db.Close() }()
	err = db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM sessions), (SELECT count(*) FROM events)").Scan(&sessions, &events)
	if err != nil {
		return 0, 0, fmt.Errorf("pool: %s: %w", a.Host, err)
	}
	return sessions, events, nil
}
