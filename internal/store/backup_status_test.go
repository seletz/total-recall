package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backupStatusEnv writes a database with one backup and returns the store,
// the database path and the backup directory.
func backupStatusEnv(t *testing.T) (*Store, string, string, time.Time) {
	t.Helper()
	s, path := openTestStore(t)
	dir := filepath.Join(t.TempDir(), "backups")
	now := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	if _, err := s.Backup(ctx, dir, now, 7, 0); err != nil {
		t.Fatal(err)
	}
	return s, path, dir, now
}

// unreadable makes dir unreadable (the macOS privacy block looks the same
// to the process: listing fails with a permission error).
func unreadable(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// TestReadBackupStatus covers the three states: a readable directory with a
// backup (ok), without one (none), and one that cannot be read (unknown, the
// time from the status file next to the database when there is one).
func TestReadBackupStatus(t *testing.T) {
	t.Run("backup writes the status file", func(t *testing.T) {
		_, path, dir, now := backupStatusEnv(t)
		got := ReadBackupStatus(path, dir, time.UTC)
		if got.State != BackupOK || got.Reason != "" || got.Hint != "" || got.Size <= 0 || filepath.Base(got.Path) != BackupFileName(now) {
			t.Errorf("ok: %+v", got)
		}
		fi, err := os.Stat(BackupStatusPath(path))
		if err != nil {
			t.Fatalf("no status file: %v", err)
		}
		if fi.Size() == 0 {
			t.Error("empty status file")
		}
		note, ok := readBackupNote(path)
		if !ok || note.File != got.Path || note.Size != got.Size || !note.Time.Equal(now) {
			t.Errorf("status file %+v (%v) does not match %+v", note, ok, got)
		}
	})
	t.Run("empty directory is none, with or without a status file", func(t *testing.T) {
		_, path, dir, _ := backupStatusEnv(t) // the status file exists
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
		got := ReadBackupStatus(path, dir, time.UTC)
		if got.State != BackupNone || got.Path != "" || !got.At.IsZero() || got.Reason != "" {
			t.Errorf("empty dir with status file: %+v (the directory is the truth when readable)", got)
		}
		_ = os.Remove(BackupStatusPath(path))
		if got := ReadBackupStatus(path, dir, time.UTC); got.State != BackupNone {
			t.Errorf("empty dir without status file: %+v", got)
		}
	})
	t.Run("missing directory is none", func(t *testing.T) {
		_, path, _, _ := backupStatusEnv(t)
		if got := ReadBackupStatus(path, filepath.Join(t.TempDir(), "gone"), time.UTC); got.State != BackupNone {
			t.Errorf("%+v", got)
		}
	})
	t.Run("unreadable directory with a status file", func(t *testing.T) {
		_, path, dir, now := backupStatusEnv(t)
		unreadable(t, dir)
		got := ReadBackupStatus(path, dir, time.UTC)
		if got.State != BackupUnknown || !strings.HasPrefix(got.Reason, "no access: ") || !strings.Contains(got.Reason, "permission") || got.Hint == "" {
			t.Errorf("state/reason/hint: %+v", got)
		}
		if filepath.Base(got.Path) != BackupFileName(now) || !got.At.Equal(now) || got.Size <= 0 {
			t.Errorf("the status file gives the copy: %+v", got)
		}
	})
	t.Run("unreadable directory without a status file", func(t *testing.T) {
		_, path, dir, _ := backupStatusEnv(t)
		_ = os.Remove(BackupStatusPath(path))
		unreadable(t, dir)
		got := ReadBackupStatus(path, dir, time.UTC)
		if got.State != BackupUnknown || got.Path != "" || !got.At.IsZero() || got.Reason == "" || got.Hint == "" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("a damaged status file is ignored", func(t *testing.T) {
		_, path, dir, _ := backupStatusEnv(t)
		if err := os.WriteFile(BackupStatusPath(path), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		unreadable(t, dir)
		got := ReadBackupStatus(path, dir, time.UTC)
		if got.State != BackupUnknown || got.Path != "" || !got.At.IsZero() {
			t.Errorf("%+v", got)
		}
	})
}

// TestEnsureBackupNote: a skipped run writes the status file for the
// newest copy when there is none, and leaves a valid one alone.
func TestEnsureBackupNote(t *testing.T) {
	_, path, dir, now := backupStatusEnv(t)
	_ = os.Remove(BackupStatusPath(path))
	p, at, err := LastBackup(dir, time.UTC)
	if err != nil || p == "" {
		t.Fatalf("%q %v", p, err)
	}
	if err := EnsureBackupNote(path, p, at); err != nil {
		t.Fatal(err)
	}
	n, ok := readBackupNote(path)
	if !ok || n.File != p || n.Size <= 0 {
		t.Fatalf("note %+v (%v)", n, ok)
	}
	if err := EnsureBackupNote(path, p, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if again, _ := readBackupNote(path); !again.Time.Equal(n.Time) {
		t.Errorf("a valid note was replaced: %v -> %v (now %v)", n.Time, again.Time, now)
	}
}
