package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeBackups creates fake backup files with distinct contents, so a test
// can tell that a file was left alone.
func writeBackups(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func requireUntouched(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Errorf("%s is gone: %v", n, err)
			continue
		}
		if !bytes.Equal(b, []byte("old "+n)) {
			t.Errorf("%s was replaced", n)
		}
	}
}

// TestBackupKeepsSameDayCopies: a new copy never replaces an existing one,
// whatever the format of the copy of that day.
func TestBackupKeepsSameDayCopies(t *testing.T) {
	now := ts("2026-11-01T09:30:00Z")
	tests := []struct {
		name     string
		existing []string
		want     string
	}{
		{"empty day", nil, "archive-2026-11-01.db.zst"},
		{"same format", []string{"archive-2026-11-01.db.zst"}, "archive-2026-11-01-0930.db.zst"},
		{"gzip of the day", []string{"archive-2026-11-01.db.gz"}, "archive-2026-11-01-0930.db.zst"},
		{"plain of the day", []string{"archive-2026-11-01.db"}, "archive-2026-11-01-0930.db.zst"},
		{"all formats", []string{"archive-2026-11-01.db", "archive-2026-11-01.db.gz", "archive-2026-11-01.db.zst"}, "archive-2026-11-01-0930.db.zst"},
		{"same minute", []string{"archive-2026-11-01.db.zst", "archive-2026-11-01-0930.db.zst"}, "archive-2026-11-01-0930-2.db.zst"},
		{"same minute, other format", []string{"archive-2026-11-01.db.zst", "archive-2026-11-01-0930.db.gz"}, "archive-2026-11-01-0930-2.db.zst"},
		{"same minute, twice", []string{"archive-2026-11-01.db.zst", "archive-2026-11-01-0930.db.zst", "archive-2026-11-01-0930-2.db.zst"}, "archive-2026-11-01-0930-3.db.zst"},
		{"only a timed copy of the day", []string{"archive-2026-11-01-0800.db.zst"}, "archive-2026-11-01-0930.db.zst"},
		{"other day", []string{"archive-2026-10-31.db.zst"}, "archive-2026-11-01.db.zst"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := openTestStore(t)
			dir := t.TempDir()
			writeBackups(t, dir, tt.existing...)
			next, err := NextBackupName(dir, now)
			if err != nil || next != tt.want {
				t.Errorf("NextBackupName = %q, %v, want %q", next, err, tt.want)
			}
			p, err := s.Backup(ctx, dir, now, 7, 0)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(p) != tt.want {
				t.Errorf("Backup wrote %s, want %s", filepath.Base(p), tt.want)
			}
			requireUntouched(t, dir, tt.existing...)
			if last, _, err := LastBackup(dir); err != nil || last != p {
				t.Errorf("LastBackup = %q, %v, want %q", last, err, p)
			}
			if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) > 0 {
				t.Errorf("temp files left: %v", m)
			}
		})
	}
}

// TestBackupSameMinuteRestores: copies of one minute are all complete.
func TestBackupSameMinuteRestores(t *testing.T) {
	s, _ := openTestStore(t)
	dir := t.TempDir()
	now := ts("2026-11-01T09:30:00Z")
	var paths []string
	for i := 0; i < 4; i++ {
		p, err := s.Backup(ctx, dir, now, 7, 0)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	for i, p := range paths {
		if err := Restore(ctx, p, filepath.Join(t.TempDir(), fmt.Sprintf("r%d.db", i)), false); err != nil {
			t.Errorf("Restore %s: %v", p, err)
		}
	}
	if len(listBackups(t, dir)) != 4 {
		t.Errorf("kept %v", listBackups(t, dir))
	}
}

// TestBackupParallel: backups running at the same time get a name each.
func TestBackupParallel(t *testing.T) {
	s, _ := openTestStore(t)
	dir := t.TempDir()
	now := ts("2026-11-01T09:30:00Z")
	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Backup(ctx, dir, now, 7, 0)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := listBackups(t, dir); len(got) != n {
		t.Errorf("kept %d of %d: %v", len(got), n, got)
	}
}

// TestClaimBackup covers both ways of publishing a copy without replacing
// anything: the hard link and the reservation for file systems without one.
func TestClaimBackup(t *testing.T) {
	noLink := func(string, string) error { return errors.ErrUnsupported }
	tests := []struct {
		name string
		link func(string, string) error
	}{{"hard link", os.Link}, {"no hard links", noLink}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tmp, dst := filepath.Join(dir, "x.tmp"), filepath.Join(dir, "x")
			write := func(s string) {
				if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write("first")
			if err := claimBackup(tmp, dst, tt.link); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(tmp); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("temp file left: %v", err)
			}
			write("second")
			if err := claimBackup(tmp, dst, tt.link); !errors.Is(err, fs.ErrExist) {
				t.Errorf("claim of a taken name: %v", err)
			}
			if b, _ := os.ReadFile(dst); string(b) != "first" {
				t.Errorf("dst = %q", b)
			}
		})
	}
}

func TestBackupFilesOrder(t *testing.T) {
	dir := t.TempDir()
	writeBackups(t, dir,
		"archive-2026-10-31-2359.db.zst",
		"archive-2026-11-01.db",
		"archive-2026-11-01.db.gz",
		"archive-2026-11-01.db.zst",
		"archive-2026-11-01-0930.db.zst",
		"archive-2026-11-01-0930-2.db.zst",
		"archive-2026-11-01-0930-10.db.zst",
		"archive-2026-11-01-1400.db.zst",
		"archive-2026-11-02.db.gz",
		"archive-2026-11-01-093.db.zst", // not ours
		"archive-2026-11-01-0930-.db.zst",
		"archive-latest.db.zst",
		"notes.txt",
	)
	got, err := backupFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range got {
		names = append(names, b.name)
	}
	want := []string{
		"archive-2026-11-02.db.gz",
		"archive-2026-11-01-1400.db.zst",
		"archive-2026-11-01-0930-10.db.zst",
		"archive-2026-11-01-0930-2.db.zst",
		"archive-2026-11-01-0930.db.zst",
		"archive-2026-11-01.db.zst",
		"archive-2026-11-01.db.gz",
		"archive-2026-11-01.db",
		"archive-2026-10-31-2359.db.zst",
	}
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Errorf("order:\n got %v\nwant %v", names, want)
	}
}

// TestPruneCountsDays: keep_daily counts days, so several copies of one day
// do not push the days before out; beyond them the month keeps its newest.
func TestPruneCountsDays(t *testing.T) {
	all := []string{
		"archive-2026-09-29.db.zst",
		"archive-2026-09-30.db.gz",
		"archive-2026-09-30-1200.db.zst",
		"archive-2026-10-30.db.zst",
		"archive-2026-10-30-1200.db.zst",
		"archive-2026-10-31.db.zst",
		"archive-2026-10-31-1200.db.zst",
		"archive-2026-11-01.db.zst",
		"archive-2026-11-01-0800.db.zst",
		"archive-2026-11-01-0900.db.zst",
		"archive-2026-11-01-1000.db.zst",
	}
	tests := []struct {
		name              string
		daily, monthly    int
		want, wantRemoved []string
	}{
		{"three days", 3, 0,
			[]string{"archive-2026-09-30-1200.db.zst", "archive-2026-10-30-1200.db.zst", "archive-2026-10-30.db.zst", "archive-2026-10-31-1200.db.zst", "archive-2026-10-31.db.zst", "archive-2026-11-01-0800.db.zst", "archive-2026-11-01-0900.db.zst", "archive-2026-11-01-1000.db.zst", "archive-2026-11-01.db.zst"},
			[]string{"archive-2026-09-29.db.zst", "archive-2026-09-30.db.gz"}},
		{"one day", 1, 0,
			[]string{"archive-2026-09-30-1200.db.zst", "archive-2026-10-31-1200.db.zst", "archive-2026-11-01-0800.db.zst", "archive-2026-11-01-0900.db.zst", "archive-2026-11-01-1000.db.zst", "archive-2026-11-01.db.zst"},
			[]string{"archive-2026-09-29.db.zst", "archive-2026-09-30.db.gz", "archive-2026-10-30.db.zst", "archive-2026-10-30-1200.db.zst", "archive-2026-10-31.db.zst"}},
		{"months limited", 1, 2,
			[]string{"archive-2026-10-31-1200.db.zst", "archive-2026-11-01-0800.db.zst", "archive-2026-11-01-0900.db.zst", "archive-2026-11-01-1000.db.zst", "archive-2026-11-01.db.zst"},
			[]string{"archive-2026-09-29.db.zst", "archive-2026-09-30.db.gz", "archive-2026-09-30-1200.db.zst", "archive-2026-10-30.db.zst", "archive-2026-10-30-1200.db.zst", "archive-2026-10-31.db.zst"}},
		{"no daily", 0, 1,
			[]string{"archive-2026-11-01-1000.db.zst"},
			[]string{"archive-2026-09-29.db.zst", "archive-2026-09-30.db.gz", "archive-2026-09-30-1200.db.zst", "archive-2026-10-30.db.zst", "archive-2026-10-30-1200.db.zst", "archive-2026-10-31.db.zst", "archive-2026-10-31-1200.db.zst", "archive-2026-11-01.db.zst", "archive-2026-11-01-0800.db.zst", "archive-2026-11-01-0900.db.zst"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeBackups(t, dir, all...)
			writeBackups(t, dir, "notes.txt")
			removed, err := PruneBackups(dir, tt.daily, tt.monthly)
			if err != nil {
				t.Fatal(err)
			}
			want := append(append([]string{}, tt.want...), "notes.txt")
			if got := listBackups(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("kept:\n got %v\nwant %v", got, want)
			}
			// Oldest first.
			var names []string
			for _, p := range removed {
				names = append(names, filepath.Base(p))
			}
			if strings.Join(names, " ") != strings.Join(tt.wantRemoved, " ") {
				t.Errorf("removed:\n got %v\nwant %v", names, tt.wantRemoved)
			}
		})
	}
}

// TestBackupLogsPruned: every copy retention deletes leaves a log line,
// and no copy leaves one without being deleted.
func TestBackupLogsPruned(t *testing.T) {
	var lines []string
	logf := func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
	s, _ := openTestStore(t)
	s.logf = logf
	dir := t.TempDir()
	writeBackups(t, dir, "archive-2026-10-29.db.gz", "archive-2026-10-30.db", "archive-2026-10-31.db.zst", "archive-2026-11-01.db.gz")
	if _, err := s.Backup(ctx, dir, ts("2026-11-01T22:00:00Z"), 2, 0); err != nil {
		t.Fatal(err)
	}
	var pruned []string
	for _, l := range lines {
		if strings.Contains(l, "pruned") {
			pruned = append(pruned, l)
		}
	}
	if len(pruned) != 2 || !strings.Contains(pruned[0], filepath.Join(dir, "archive-2026-10-29.db.gz")) || !strings.Contains(pruned[1], filepath.Join(dir, "archive-2026-10-30.db")) {
		t.Errorf("log lines = %q", pruned)
	}
	requireUntouched(t, dir, "archive-2026-10-31.db.zst", "archive-2026-11-01.db.gz")
}

// TestLastBackupTimedName: the timed copy is newer than the one of the plain name.
func TestLastBackupTimedName(t *testing.T) {
	dir := t.TempDir()
	writeBackups(t, dir, "archive-2026-11-01.db.zst", "archive-2026-11-01-0930.db.zst")
	mt := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "archive-2026-11-01-0930.db.zst"), mt, mt); err != nil {
		t.Fatal(err)
	}
	p, got, err := LastBackup(dir)
	if err != nil || filepath.Base(p) != "archive-2026-11-01-0930.db.zst" || !got.Equal(mt) {
		t.Errorf("LastBackup = %q %v %v", p, got, err)
	}
}
