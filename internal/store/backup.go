package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var backupName = regexp.MustCompile(`^archive-(\d{4}-\d{2})-\d{2}\.db$`)

// BackupFileName returns the backup file name for the calendar day of now
// (in now's location).
func BackupFileName(now time.Time) string {
	return "archive-" + now.Format("2006-01-02") + ".db"
}

// Backup writes a consistent copy of the database to
// dir/archive-YYYY-MM-DD.db using VACUUM INTO (replacing a copy from the same
// day), then applies retention: keep the keepDaily newest copies plus the
// newest copy of each month, limited to the keepMonthly newest months
// (0 = all months). It returns the path of the new copy.
func (s *Store) Backup(ctx context.Context, dir string, now time.Time, keepDaily, keepMonthly int) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	dst := filepath.Join(dir, BackupFileName(now))
	tmp := dst + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("store: backup: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("store: backup: %w", err)
	}
	if _, err := PruneBackups(dir, keepDaily, keepMonthly); err != nil {
		return dst, err
	}
	return dst, nil
}

// backupFiles returns backup file names in dir, newest first. A missing dir
// yields no files.
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
// policy (see Backup) and returns the removed paths. Other files are never touched.
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

// LastBackup returns the newest backup copy in dir and its modification
// time. path is empty if there is none (also if dir does not exist).
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
