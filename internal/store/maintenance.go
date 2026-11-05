package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// This file implements `total-recall maintenance`: rebuild the archive with
// a new page size on a copy and swap it in atomically, while writers (hooks)
// are kept out by a lock (see docs/explanation/design.md, "Wartung").
//
// Locking: every read-write open with a lock dir (WithLockDir) holds a
// shared flock on <lockdir>/maintenance.lock until Close; Maintain holds it
// exclusively from the checkpoint to the swap. A writer that meets the
// exclusive lock fails at once with ErrMaintenance instead of waiting, so a
// hook logs and exits and the next hook reads from the stored offset. The
// lock exists because a process that already had the old file open would
// otherwise keep writing into the old inode after the swap.
//
// flock has no writer preference: while writers keep reopening, an
// exclusive request could starve. So Maintain first takes
// <lockdir>/maintenance.pending exclusively, for the wait and the run, and
// a writer takes its shared lock only after it found the pending file free.
// Once maintenance waits, no new writer starts; it only has to outlast the
// writers that were already open.

// MaintenanceLockName is the lock file in the lock dir shared by writers
// (shared lock) and maintenance (exclusive lock).
const MaintenanceLockName = "maintenance.lock"

// MaintenancePendingName is the lock file maintenance holds exclusively
// while it waits for writers and during the run; a writer that finds it
// locked skips.
const MaintenancePendingName = "maintenance.pending"

// Errors of Open with a lock dir and of Maintain.
var (
	// ErrMaintenance means a maintenance run holds the archive; the writer
	// must not open it now.
	ErrMaintenance = errors.New("store: maintenance in progress, the archive is being rebuilt")
	// ErrWritersActive means writers (hooks) still held the archive when the
	// wait for the maintenance lock ran out.
	ErrWritersActive = errors.New("store: writers active: an ingest or backup still has the archive open; wait for it or raise --wait")
	// ErrReadersActive means readers prevented the WAL checkpoint before the
	// swap.
	ErrReadersActive = errors.New("store: readers active: an open TUI or another reading process blocks the checkpoint; close it and retry")
	// ErrDiskSpace means there is not enough free space for the copies.
	ErrDiskSpace = errors.New("store: not enough free disk space")
)

// WithLockDir makes Open hold the shared maintenance lock in dir (see
// MaintenanceLockName) until Close. Open fails with ErrMaintenance while a
// maintenance run holds the lock exclusively.
func WithLockDir(dir string) Option { return func(s *Store) { s.lockDir = dir } }

// openLockFile opens (creating) the lock file name in dir.
func openLockFile(dir, name string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("store: lock dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("store: lock: %w", err)
	}
	return f, nil
}

// flockNB tries a non-blocking flock with how on f. ok is false when another
// process holds a conflicting lock.
func flockNB(f *os.File, how int) (ok bool, err error) {
	if err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, fmt.Errorf("store: lock: %w", err)
	}
	return true, nil
}

func releaseLock(f *os.File) func() {
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}

// lockShared takes the writer's shared lock without waiting. It fails with
// ErrMaintenance when maintenance runs or is waiting for writers.
func lockShared(dir string) (release func(), err error) {
	pending, err := openLockFile(dir, MaintenancePendingName)
	if err != nil {
		return nil, err
	}
	ok, err := flockNB(pending, syscall.LOCK_SH)
	_ = pending.Close() // releases the probe lock
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrMaintenance
	}
	f, err := openLockFile(dir, MaintenanceLockName)
	if err != nil {
		return nil, err
	}
	ok, err = flockNB(f, syscall.LOCK_SH)
	if err != nil || !ok {
		_ = f.Close()
		if err == nil {
			err = ErrMaintenance
		}
		return nil, err
	}
	return releaseLock(f), nil
}

// lockMaintenance announces the run (exclusive pending lock, so that no new
// writer starts) and then takes the exclusive lock, retrying for up to wait
// while the writers that were already open hold it; it fails with
// ErrWritersActive otherwise. release gives up both.
func lockMaintenance(dir string, wait time.Duration) (release func(), err error) {
	pending, err := openLockFile(dir, MaintenancePendingName)
	if err != nil {
		return nil, err
	}
	ok, err := flockNB(pending, syscall.LOCK_EX)
	if err != nil || !ok {
		_ = pending.Close()
		if err == nil {
			err = fmt.Errorf("%w (another maintenance run)", ErrMaintenance)
		}
		return nil, err
	}
	releasePending := releaseLock(pending)
	f, err := openLockFile(dir, MaintenanceLockName)
	if err != nil {
		releasePending()
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		ok, err := flockNB(f, syscall.LOCK_EX)
		if err != nil {
			_ = f.Close()
			releasePending()
			return nil, err
		}
		if ok {
			releaseMain := releaseLock(f)
			return func() { releaseMain(); releasePending() }, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			releasePending()
			return nil, ErrWritersActive
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// PageStats describes the physical state of an archive file. SizeBytes is
// the database file plus its WAL. The counts let a rebuilt copy be compared
// with its original.
type PageStats struct {
	SizeBytes     int64 `json:"size_bytes"`
	PageSize      int   `json:"page_size"`
	PageCount     int64 `json:"page_count"`
	FreelistPages int64 `json:"freelist_pages"`
	Sessions      int   `json:"sessions"`
	Events        int   `json:"events"`
	Sources       int   `json:"sources"`
	Meta          int   `json:"meta"`
	// Blobs is the number of stored attachments (0 before schema v6).
	Blobs int `json:"blobs"`
}

// PageStats reads the page size, page and freelist counts and the row
// counts of the open archive.
func (s *Store) PageStats(ctx context.Context) (PageStats, error) {
	var ps PageStats
	for _, q := range []struct {
		sql string
		dst any
	}{
		{"PRAGMA page_size", &ps.PageSize},
		{"PRAGMA page_count", &ps.PageCount},
		{"PRAGMA freelist_count", &ps.FreelistPages},
		{"SELECT count(*) FROM sessions", &ps.Sessions},
		{"SELECT count(*) FROM events", &ps.Events},
		{"SELECT count(*) FROM sources", &ps.Sources},
		// An archive that still waits for migration 6 has no blobs table
		// (status reads PageStats on it): count 0 then.
		{"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'blobs'", &ps.Blobs},
	} {
		if err := s.db().QueryRowContext(ctx, q.sql).Scan(q.dst); err != nil {
			return ps, fmt.Errorf("store: page stats: %s: %w", q.sql, err)
		}
	}
	if ps.Blobs > 0 {
		if err := s.db().QueryRowContext(ctx, "SELECT count(*) FROM blobs").Scan(&ps.Blobs); err != nil {
			return ps, fmt.Errorf("store: page stats: blobs: %w", err)
		}
	}
	// session_meta arrives with v7; status reads an outdated archive too,
	// and SQLite rejects a statement that names a missing table.
	var hasMeta int
	if err := s.db().QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'session_meta'").Scan(&hasMeta); err != nil {
		return ps, fmt.Errorf("store: page stats: %w", err)
	}
	if hasMeta > 0 {
		if err := s.db().QueryRowContext(ctx, "SELECT count(*) FROM session_meta").Scan(&ps.Meta); err != nil {
			return ps, fmt.Errorf("store: page stats: %w", err)
		}
	}
	ps.SizeBytes = fileSize(s.path)
	return ps, nil
}

// fileSize returns the size of a database file plus its WAL (0 if missing).
func fileSize(path string) int64 {
	var n int64
	for _, sfx := range []string{"", "-wal"} {
		if fi, err := os.Stat(path + sfx); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// MaintainOptions configure Maintain.
type MaintainOptions struct {
	// PageSize is the page size of the rebuilt archive: a power of two
	// between 512 and 65536.
	PageSize int
	// DryRun builds the copy to measure it and deletes it again: no backup,
	// no lock, the archive is not touched.
	DryRun bool
	// LockDir holds the maintenance lock (the ingest lock dir).
	LockDir string
	// Wait is how long to wait for writers to release the lock.
	Wait time.Duration
	// BackupDir, Now, KeepDaily and KeepMonthly are passed to Backup, which
	// runs first; an empty BackupDir skips the backup.
	BackupDir   string
	Now         time.Time
	KeepDaily   int
	KeepMonthly int
	// Logf receives one line per step; default discard.
	Logf func(format string, args ...any)
}

// MaintainResult reports what Maintain did. After describes the rebuilt
// archive, in a dry run the measured copy that was deleted again.
type MaintainResult struct {
	DryRun   bool
	Swapped  bool
	Backup   string
	Before   PageStats
	After    PageStats
	Duration time.Duration
}

// checkCopy verifies a rebuilt copy: PRAGMA integrity_check and the row
// counts of want. It returns the copy's stats. A variable so tests can make
// it fail.
var checkCopy = func(ctx context.Context, path string, want PageStats) (PageStats, error) {
	st, err := OpenReadOnly(path)
	if err != nil {
		return PageStats{}, err
	}
	defer func() { _ = st.Close() }()
	rows, err := st.db().QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return PageStats{}, fmt.Errorf("integrity check: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return PageStats{}, fmt.Errorf("integrity check: %w", err)
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return PageStats{}, fmt.Errorf("integrity check: %w", err)
	}
	if len(problems) > 0 {
		return PageStats{}, fmt.Errorf("integrity check: %s", strings.Join(problems, "; "))
	}
	got, err := st.PageStats(ctx)
	if err != nil {
		return PageStats{}, err
	}
	if got.Sessions != want.Sessions || got.Events != want.Events || got.Sources != want.Sources || got.Blobs != want.Blobs || got.Meta != want.Meta {
		return PageStats{}, fmt.Errorf("row counts differ: copy has %d sessions, %d events, %d sources, %d blobs, %d meta records; original %d, %d, %d, %d, %d",
			got.Sessions, got.Events, got.Sources, got.Blobs, got.Meta, want.Sessions, want.Events, want.Sources, want.Blobs, want.Meta)
	}
	return got, nil
}

// freeSpace returns the free bytes available to this user on the file
// system of dir. A variable so tests can simulate a full disk.
var freeSpace = func(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:unconvert // Bsize differs in type per platform
}

// tempPattern names the temporary copies next to the database; leftovers
// of an earlier run are removed at the start of the next.
const tempPattern = ".maint-*"

// Maintain rebuilds the archive at path with o.PageSize (see
// MaintainOptions): backup, exclusive lock, WAL checkpoint, PRAGMA page_size
// + VACUUM INTO a temporary copy next to the database, integrity check and
// row counts of the copy, then an atomic rename over the original with its
// -wal/-shm removed. Any failure before the rename leaves the original
// untouched and removes the copy; leftovers of an interrupted run are
// removed at the next start.
func Maintain(ctx context.Context, path string, o MaintainOptions) (MaintainResult, error) {
	start := time.Now()
	res := MaintainResult{DryRun: o.DryRun}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if err := validPageSize(o.PageSize); err != nil {
		return res, err
	}
	if _, err := os.Stat(path); err != nil {
		return res, fmt.Errorf("store: maintenance: %w", err)
	}
	dir, base := filepath.Dir(path), filepath.Base(path)
	if err := removeLeftovers(dir, base); err != nil {
		return res, err
	}
	if err := checkDiskSpace(path, o); err != nil {
		return res, err
	}

	if !o.DryRun && o.BackupDir != "" {
		st, err := Open(path, WithLogf(o.Logf))
		if err != nil {
			return res, err
		}
		p, err := st.Backup(ctx, o.BackupDir, o.Now, o.KeepDaily, o.KeepMonthly)
		_ = st.Close()
		if err != nil {
			return res, err
		}
		res.Backup = p
		o.Logf("backup written: %s", p)
	}

	if !o.DryRun {
		if o.LockDir == "" {
			return res, errors.New("store: maintenance: no lock dir")
		}
		release, err := lockMaintenance(o.LockDir, o.Wait)
		if err != nil {
			return res, err
		}
		defer release()
	}

	st, err := Open(path, WithLogf(o.Logf))
	if err != nil {
		return res, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = st.Close()
		}
	}()
	if !o.DryRun {
		if err := st.checkpointTruncate(ctx); err != nil {
			return res, err
		}
	}
	res.Before, err = st.PageStats(ctx)
	if err != nil {
		return res, err
	}

	tmp, err := tempName(dir, base)
	if err != nil {
		return res, err
	}
	defer func() { _ = os.Remove(tmp) }()
	o.Logf("rebuilding with page size %d into %s", o.PageSize, tmp)
	if err := st.compactInto(ctx, o.PageSize, tmp); err != nil {
		return res, err
	}
	res.After, err = checkCopy(ctx, tmp, res.Before)
	if err != nil {
		return res, fmt.Errorf("store: maintenance: copy rejected: %w", err)
	}
	if o.DryRun {
		res.Duration = time.Since(start)
		return res, nil
	}

	// Swap. From here on the original is replaced; the copy has been
	// checked, the WAL is empty (checkpointed above, no writers), so
	// nothing is lost.
	closed = true
	if err := st.Close(); err != nil {
		return res, fmt.Errorf("store: maintenance: %w", err)
	}
	for _, sfx := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + sfx); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return res, fmt.Errorf("store: maintenance: %w", err)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		return res, fmt.Errorf("store: maintenance: swap: %w", err)
	}
	res.Swapped = true
	o.Logf("archive replaced: %s", path)

	// Open the new file once: back to WAL mode, final stats.
	st2, err := Open(path, WithLogf(o.Logf))
	if err != nil {
		return res, err
	}
	defer func() { _ = st2.Close() }()
	res.After, err = st2.PageStats(ctx)
	if err != nil {
		return res, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

// validPageSize checks that n is a power of two between 512 and 65536.
func validPageSize(n int) error {
	if n < 512 || n > 65536 || n&(n-1) != 0 {
		return fmt.Errorf("store: maintenance: page size %d is not a power of two between 512 and 65536", n)
	}
	return nil
}

// removeLeftovers deletes temporary copies of earlier runs next to the
// database.
func removeLeftovers(dir, base string) error {
	matches, err := filepath.Glob(filepath.Join(dir, base+tempPattern))
	if err != nil {
		return fmt.Errorf("store: maintenance: %w", err)
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("store: maintenance: remove leftover: %w", err)
		}
	}
	return nil
}

// tempName reserves a unique name for the copy next to the database. The
// file is removed again: VACUUM INTO needs a name that does not exist.
func tempName(dir, base string) (string, error) {
	f, err := os.CreateTemp(dir, base+tempPattern)
	if err != nil {
		return "", fmt.Errorf("store: maintenance: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("store: maintenance: %w", err)
	}
	return name, nil
}

// checkDiskSpace fails with ErrDiskSpace unless the file system of the
// database has room for the copies: the archive size once for a dry run,
// twice for a full run (backup copy, then the rebuilt copy), each with a
// quarter of headroom; the backup dir needs half the size for the
// compressed copy.
func checkDiskSpace(path string, o MaintainOptions) error {
	size := fileSize(path)
	need := size + size/4
	if !o.DryRun {
		need = 2*size + size/4
	}
	free, err := freeSpace(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("store: maintenance: %w", err)
	}
	if free < need {
		return fmt.Errorf("%w next to the database: %s free, about %s needed for the copies", ErrDiskSpace, mib(free), mib(need))
	}
	if o.DryRun || o.BackupDir == "" {
		return nil
	}
	if err := os.MkdirAll(o.BackupDir, 0o755); err != nil {
		return fmt.Errorf("store: maintenance: backup dir: %w", err)
	}
	free, err = freeSpace(o.BackupDir)
	if err != nil {
		return fmt.Errorf("store: maintenance: %w", err)
	}
	if free < size/2 {
		return fmt.Errorf("%w in the backup dir %s: %s free, about %s needed", ErrDiskSpace, o.BackupDir, mib(free), mib(size/2))
	}
	return nil
}

func mib(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20)) }

// checkpointTruncate moves every WAL frame into the database and truncates
// the WAL, so that the file can be replaced without a stale WAL being
// paired with the new file. Readers that keep the WAL in use make it fail
// with ErrReadersActive.
func (s *Store) checkpointTruncate(ctx context.Context) error {
	var busy, logFrames, done int
	if err := s.db().QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &done); err != nil {
		return fmt.Errorf("store: maintenance: checkpoint: %w", err)
	}
	if busy != 0 {
		return ErrReadersActive
	}
	if fi, err := os.Stat(s.path + "-wal"); err == nil && fi.Size() > 0 {
		return fmt.Errorf("%w (WAL not empty after checkpoint)", ErrReadersActive)
	}
	return nil
}

// compactInto writes a copy of the archive with pageSize to dst (which must
// not exist) with PRAGMA page_size and VACUUM INTO on one connection.
func (s *Store) compactInto(ctx context.Context, pageSize int, dst string) error {
	conn, err := s.db().Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: maintenance: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA page_size=%d", pageSize)); err != nil {
		return fmt.Errorf("store: maintenance: page size: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("store: maintenance: vacuum into: %w", err)
	}
	return nil
}
