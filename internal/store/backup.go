package store

import (
	"cmp"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// backupName matches backup copies: the first copy of a day
// (archive-YYYY-MM-DD.db.zst), a further one with the time of day
// (archive-YYYY-MM-DD-HHMM.db.zst) and a further one of the same minute with
// a number (archive-YYYY-MM-DD-HHMM-N.db.zst). The format is zstd (current),
// gzip (up to v1.2) or a plain SQLite file (older versions).
// Groups: date, time, number, format.
var backupName = regexp.MustCompile(`^archive-(\d{4}-\d{2}-\d{2})(?:-(\d{4})(?:-(\d+))?)?\.db(\.gz|\.zst)?$`)

// backupEncoderConcurrency bounds the goroutines (and so the memory) of the
// zstd encoder: the backup runs from a hook next to the user's work.
const backupEncoderConcurrency = 4

// backupFormats are the file name endings of a backup copy after ".db".
var backupFormats = []string{"", ".gz", ".zst"}

// BackupFileName returns the name of the first backup copy of the calendar
// day of now (in now's location). NextBackupName takes the copies already
// there into account.
func BackupFileName(now time.Time) string {
	return backupStem(now, 0) + ".zst"
}

// backupStem is the name without the format of the n-th candidate for a
// copy at now: the day, the day and the time, then a running number.
func backupStem(now time.Time, n int) string {
	stem := "archive-" + now.Format("2006-01-02")
	switch n {
	case 0:
	case 1:
		stem += "-" + now.Format("1504")
	default:
		stem += fmt.Sprintf("-%s-%d", now.Format("1504"), n)
	}
	return stem + ".db"
}

// freeBackupName returns the first candidate from index n on that is not
// taken by copies, in any format, and the index after it. The name of the
// day is free only for the first copy of the day, so a name carries the
// time of every later one.
func freeBackupName(copies []backupFile, now time.Time, n int) (string, int) {
	day := now.Format("2006-01-02")
	taken := map[string]bool{}
	dayTaken := false
	for _, c := range copies {
		taken[c.name] = true
		dayTaken = dayTaken || c.day == day
	}
next:
	for ; ; n++ {
		stem := backupStem(now, n)
		if n == 0 && dayTaken {
			continue
		}
		for _, f := range backupFormats {
			if taken[stem+f] {
				continue next
			}
		}
		return stem + ".zst", n + 1
	}
}

// NextBackupName returns the name the next backup copy in dir would get at
// now (in now's location): see BackupFileName, and with a copy of that day
// in any format the first free name with the time of day.
func NextBackupName(dir string, now time.Time) (string, error) {
	copies, err := backupFiles(dir, now.Location())
	if err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	name, _ := freeBackupName(copies, now, 0)
	return name, nil
}

// Backup writes a consistent, zstd-compressed copy of the database to dir
// under NextBackupName, then applies retention: keep all copies of the
// keepDaily newest days plus the newest copy of each month, limited to the
// keepMonthly newest months (0 = all months), judged in the zone of now,
// which is the zone of the names. It returns the path of the new copy. A new copy never replaces an existing one; only retention deletes
// copies, and it logs each one.
//
// The copy is made with VACUUM INTO into a temporary file next to the
// database (not in dir, which may be synced), compressed into a temporary
// file in dir and moved to its final name, so dir never holds a partial
// backup under a final name.
func (s *Store) Backup(ctx context.Context, dir string, now time.Time, keepDaily, keepMonthly int) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
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
	if _, err := s.db().ExecContext(ctx, "VACUUM INTO ?", tmpDB); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	tf, err := os.CreateTemp(dir, "archive-*.tmp")
	if err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	tmp := tf.Name()
	_ = tf.Close()
	defer func() { _ = os.Remove(tmp) }()
	// CreateTemp makes the file private; a backup has the usual mode.
	if err := os.Chmod(tmp, 0o644); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	if err := zstdFile(tmpDB, tmp); err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	dst, err := publishBackup(tmp, dir, now)
	if err != nil {
		return "", fmt.Errorf("store: backup: %w", err)
	}
	if err := writeBackupNote(s.path, dst, now); err != nil {
		s.logf("backup: status file next to the database not written: %v", err)
	}
	removed, err := PruneBackups(dir, keepDaily, keepMonthly, now.Location())
	for _, p := range removed {
		s.logf("backup: pruned %s (retention: keep_daily %d, keep_monthly %d)", p, keepDaily, keepMonthly)
	}
	if err != nil {
		return dst, err
	}
	return dst, nil
}

// publishBackup gives the finished copy tmp its final name and returns the
// path. A parallel backup can take a name between the check and the claim;
// then the next candidate is tried.
func publishBackup(tmp, dir string, now time.Time) (string, error) {
	copies, err := backupFiles(dir, now.Location())
	if err != nil {
		return "", err
	}
	for n := 0; ; {
		var name string
		name, n = freeBackupName(copies, now, n)
		dst := filepath.Join(dir, name)
		err := claimBackup(tmp, dst, os.Link)
		if err == nil {
			return dst, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
}

// claimBackup moves tmp to dst unless dst exists (then the error matches
// fs.ErrExist and tmp stays): rename alone would replace it. link is the
// atomic way; a file system without hard links gets dst reserved with an
// empty file that the rename then replaces.
func claimBackup(tmp, dst string, link func(oldname, newname string) error) error {
	err := link(tmp, dst)
	if err == nil {
		return os.Remove(tmp)
	}
	if errors.Is(err, fs.ErrExist) {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_ = f.Close()
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

// zstdFile compresses src into a new file dst and syncs it. The level is
// SpeedBetterCompression: on a real archive it was a tenth smaller than
// SpeedDefault for less than a second more, while SpeedBestCompression took
// ten times as long for another 5 %.
func zstdFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	zw, err := zstd.NewWriter(out,
		zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
		zstd.WithEncoderConcurrency(backupEncoderConcurrency))
	if err != nil {
		_ = out.Close()
		return err
	}
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
// is a zstd-compressed copy (*.zst), a gzip-compressed one (*.gz, up to
// v1.2) or an uncompressed SQLite file (older versions). dst must
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
	r, closeReader, err := backupReader(src, in)
	if err != nil {
		_ = tmpf.Close()
		cleanup()
		return fmt.Errorf("store: restore %s: %w", src, err)
	}
	defer closeReader()
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

// backupReader wraps in with the decompressor the backup file name asks for:
// zstd for .zst, gzip for .gz, none otherwise. A decoding error (a file that
// is not what its name says) surfaces from the first read.
func backupReader(name string, in io.Reader) (io.Reader, func(), error) {
	switch {
	case strings.HasSuffix(name, ".zst"):
		zr, err := zstd.NewReader(in, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, nil, err
		}
		return zr, zr.Close, nil
	case strings.HasSuffix(name, ".gz"):
		zr, err := gzip.NewReader(in)
		if err != nil {
			return nil, nil, err
		}
		return zr, func() { _ = zr.Close() }, nil
	}
	return in, func() {}, nil
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
	if err := st.db().QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if res != "ok" {
		return fmt.Errorf("integrity check: %s", res)
	}
	return nil
}

// backupFile is a backup copy in a directory.
type backupFile struct {
	name string
	// day is YYYY-MM-DD, month YYYY-MM.
	day, month string
	// clock is HHMM, empty for the first copy of a day; number counts the
	// copies of one minute.
	clock  string
	number int
	// format ranks the copies of the same name: .zst, .gz, plain.
	format int
	// named is what the name says: the day and the clock, without a clock
	// the start of the day.
	named time.Time
	// at is when the copy was made, see backupTime.
	at time.Time
}

// parseBackupName returns the copy a file name stands for; the clock in a
// name is local to loc. A name without a real date and time is no copy.
func parseBackupName(name string, loc *time.Location) (backupFile, bool) {
	m := backupName.FindStringSubmatch(name)
	if m == nil {
		return backupFile{}, false
	}
	stamp, layout := m[1], "2006-01-02"
	if m[2] != "" {
		stamp, layout = stamp+"-"+m[2], layout+"-1504"
	}
	named, err := time.ParseInLocation(layout, stamp, loc)
	if err != nil {
		return backupFile{}, false
	}
	b := backupFile{name: name, day: m[1], month: m[1][:7], clock: m[2], named: named}
	if m[3] != "" {
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return backupFile{}, false
		}
		b.number = n
	}
	b.format = slices.Index(backupFormats, m[4])
	return b, true
}

// backupTime returns when copy b was made, and is the only place that
// decides it: the date and time in the name, or, for a name with only a
// date, the modification time mtime. An mtime outside the named day says
// nothing about the copy (it was copied or restored later), then the start
// of the day stands in. So a copy never leaves the day of its name.
func backupTime(b backupFile, mtime time.Time) time.Time {
	if b.clock == "" && mtime.In(b.named.Location()).Format("2006-01-02") == b.day {
		return mtime
	}
	return b.named
}

// compareBackups orders copies oldest first: by time, to the minute, the
// precision of a timed name. Within a minute the copy without a time was the
// first one, then the number decides; of the same name .db, .gz, .zst.
func compareBackups(a, b backupFile) int {
	return cmp.Or(a.at.Truncate(time.Minute).Compare(b.at.Truncate(time.Minute)),
		cmp.Compare(a.clock, b.clock), cmp.Compare(a.number, b.number), cmp.Compare(a.format, b.format))
}

// backupFiles returns the backup copies in dir, newest first; loc is the
// time zone of their names. A missing dir yields no files.
func backupFiles(dir string, loc *time.Location) ([]backupFile, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []backupFile
	for _, e := range ents {
		b, ok := parseBackupName(e.Name(), loc)
		if !ok || e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue // retention of a parallel backup was faster
		}
		if err != nil {
			return nil, err
		}
		b.at = backupTime(b, fi.ModTime())
		files = append(files, b)
	}
	slices.SortFunc(files, func(a, b backupFile) int { return compareBackups(b, a) })
	return files, nil
}

// PruneBackups deletes backup copies in dir that fall outside the retention
// policy (see Backup) and returns the removed paths, oldest first. keepDaily
// counts days: all copies of a kept day stay. Every format and name counts
// alike, in the order of their time (see backupTime); loc is the time zone
// of the names. Other files are never touched.
func PruneBackups(dir string, keepDaily, keepMonthly int, loc *time.Location) ([]string, error) {
	files, err := backupFiles(dir, loc)
	if err != nil {
		return nil, fmt.Errorf("store: prune backups: %w", err)
	}
	keep := map[string]bool{}
	days := map[string]bool{}
	for _, f := range files {
		if !days[f.day] && len(days) == keepDaily {
			break
		}
		days[f.day] = true
		keep[f.name] = true
	}
	months := map[string]bool{}
	for _, f := range files {
		if months[f.month] {
			continue
		}
		if keepMonthly > 0 && len(months) == keepMonthly {
			break
		}
		months[f.month] = true
		keep[f.name] = true
	}
	var removed []string
	for _, f := range slices.Backward(files) {
		if keep[f.name] {
			continue
		}
		p := filepath.Join(dir, f.name)
		if err := os.Remove(p); errors.Is(err, fs.ErrNotExist) {
			continue // a parallel backup was faster
		} else if err != nil {
			return removed, fmt.Errorf("store: prune backups: %w", err)
		}
		removed = append(removed, p)
	}
	return removed, nil
}

// LastBackup returns the newest backup copy in dir (in any of the formats
// backupName matches) and when it was made (see backupTime); loc is the time
// zone of the names. path is empty if there is none (also if dir does not
// exist).
func LastBackup(dir string, loc *time.Location) (path string, at time.Time, err error) {
	files, err := backupFiles(dir, loc)
	if err != nil || len(files) == 0 {
		return "", time.Time{}, err
	}
	return filepath.Join(dir, files[0].name), files[0].at, nil
}
