package store

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// backupName matches backup copies: gzip-compressed (current) and plain
// SQLite files (written by older versions). Group 1 is the month.
var backupName = regexp.MustCompile(`^archive-(\d{4}-\d{2})-\d{2}\.db(\.gz)?$`)

// BackupFileName returns the backup file name for the calendar day of now
// (in now's location).
func BackupFileName(now time.Time) string {
	return legacyBackupFileName(now) + ".gz"
}

func legacyBackupFileName(now time.Time) string {
	return "archive-" + now.Format("2006-01-02") + ".db"
}

// Backup writes a consistent, gzip-compressed copy of the database to
// dir/archive-YYYY-MM-DD.db.gz (replacing a copy from the same day, also an
// uncompressed one from older versions), then applies retention: keep the
// keepDaily newest copies plus the newest copy of each month, limited to the
// keepMonthly newest months (0 = all months). It returns the path of the new
// copy.
//
// The copy is made with VACUUM INTO into a temporary file next to the
// database (not in dir, which may be synced), compressed into dir/<name>.tmp
// and renamed into place, so dir never holds a partial backup under the
// final name.
func (s *Store) Backup(ctx context.Context, dir string, now time.Time, keepDaily, keepMonthly int) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	dst := filepath.Join(dir, BackupFileName(now))
	// A unique name, so parallel backups do not collide; VACUUM INTO needs
	// a file that does not exist yet.
	f, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".backup-*")
	if err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	tmpDB := f.Name()
	_ = f.Close()
	if err := os.Remove(tmpDB); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	defer func() { _ = os.Remove(tmpDB) }()
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", tmpDB); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	tmp := fmt.Sprintf("%s.%d.tmp", dst, os.Getpid())
	if err := gzipFile(tmpDB, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("store: backup: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("store: backup: %w", err)
	}
	// The compressed copy supersedes an uncompressed one of the same day.
	if err := os.Remove(filepath.Join(dir, legacyBackupFileName(now))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return dst, fmt.Errorf("store: backup: %w", err)
	}
	if _, err := PruneBackups(dir, keepDaily, keepMonthly); err != nil {
		return dst, err
	}
	return dst, nil
}

// gzipFile compresses src into a new file dst and syncs it.
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Restore writes the database contained in the backup file src to dst. src
// is a gzip-compressed copy (*.gz) or an uncompressed SQLite file. dst must
// not exist unless force is set; with force, dst and its -wal/-shm files are
// replaced. The data is first written to a temporary file next to dst and
// checked (schema version, PRAGMA quick_check); dst is only touched if the
// check passes.
func Restore(ctx context.Context, src, dst string, force bool) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	defer func() { _ = in.Close() }()
	if _, err := os.Lstat(dst); err == nil && !force {
		return fmt.Errorf("store: restore: %s exists (use --force to replace it)", dst)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("store: restore: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	tmpf, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".restore-*")
	if err != nil {
		return fmt.Errorf("store: restore: %w", err)
	}
	tmp := tmpf.Name()
	cleanup := func() {
		for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(tmp + sfx)
		}
	}
	var r io.Reader = in
	if strings.HasSuffix(src, ".gz") {
		zr, err := gzip.NewReader(in)
		if err != nil {
			_ = tmpf.Close()
			cleanup()
			return fmt.Errorf("store: restore %s: %w", src, err)
		}
		defer func() { _ = zr.Close() }()
		r = zr
	}
	_, err = io.Copy(tmpf, r)
	if err == nil {
		err = tmpf.Sync()
	}
	if cerr := tmpf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return fmt.Errorf("store: restore %s: %w", src, err)
	}
	if err := checkDatabase(ctx, tmp); err != nil {
		cleanup()
		return fmt.Errorf("store: restore %s: %w", src, err)
	}
	if force {
		for _, sfx := range []string{"-wal", "-shm"} {
			if err := os.Remove(dst + sfx); err != nil && !errors.Is(err, fs.ErrNotExist) {
				cleanup()
				return fmt.Errorf("store: restore: %w", err)
			}
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		cleanup()
		return fmt.Errorf("store: restore: %w", err)
	}
	cleanup()
	return nil
}

// checkDatabase verifies that path is a total-recall database this binary
// can read and that SQLite finds no corruption.
func checkDatabase(ctx context.Context, path string) error {
	st, err := OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	v, err := st.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("not a total-recall database: %w", err)
	}
	if v == 0 {
		return fmt.Errorf("not a total-recall database (no schema version)")
	}
	var res string
	if err := st.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if res != "ok" {
		return fmt.Errorf("integrity check: %s", res)
	}
	return nil
}

// backupFiles returns backup file names in dir, newest first (for the same
// day the compressed copy first). A missing dir yields no files.
func backupFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && backupName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	// The date in the name sorts lexicographically.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// PruneBackups deletes backup copies in dir that fall outside the retention
// policy (see Backup) and returns the removed paths. Compressed and legacy
// uncompressed copies count alike. Other files are never touched.
func PruneBackups(dir string, keepDaily, keepMonthly int) ([]string, error) {
	names, err := backupFiles(dir)
	if err != nil {
		return nil, fmt.Errorf("store: prune backups: %w", err)
	}
	keep := map[string]bool{}
	for i := 0; i < keepDaily && i < len(names); i++ {
		keep[names[i]] = true
	}
	months := 0
	seen := map[string]bool{}
	for _, n := range names {
		m := backupName.FindStringSubmatch(n)[1]
		if seen[m] {
			continue
		}
		seen[m] = true
		months++
		if keepMonthly > 0 && months > keepMonthly {
			break
		}
		keep[n] = true
	}
	var removed []string
	for _, n := range names {
		if keep[n] {
			continue
		}
		p := filepath.Join(dir, n)
		if err := os.Remove(p); err != nil {
			return removed, fmt.Errorf("store: prune backups: %w", err)
		}
		removed = append(removed, p)
	}
	return removed, nil
}

// LastBackup returns the newest backup copy in dir (compressed or legacy
// uncompressed) and its modification time. path is empty if there is none
// (also if dir does not exist).
func LastBackup(dir string) (path string, modTime time.Time, err error) {
	names, err := backupFiles(dir)
	if err != nil || len(names) == 0 {
		return "", time.Time{}, err
	}
	p := filepath.Join(dir, names[0])
	fi, err := os.Stat(p)
	if err != nil {
		return "", time.Time{}, err
	}
	return p, fi.ModTime(), nil
}
