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
		got := ReadBackupStatus(probe(path, dir))
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
		got := ReadBackupStatus(probe(path, dir))
		if got.State != BackupNone || got.Path != "" || !got.At.IsZero() || got.Reason != "" {
			t.Errorf("empty dir with status file: %+v (the directory is the truth when readable)", got)
		}
		_ = os.Remove(BackupStatusPath(path))
		if got := ReadBackupStatus(probe(path, dir)); got.State != BackupNone {
			t.Errorf("empty dir without status file: %+v", got)
		}
	})
	t.Run("missing directory is none", func(t *testing.T) {
		_, path, _, _ := backupStatusEnv(t)
		if got := ReadBackupStatus(probe(path, filepath.Join(t.TempDir(), "gone"))); got.State != BackupNone {
			t.Errorf("%+v", got)
		}
	})
	t.Run("unreadable directory with a status file", func(t *testing.T) {
		_, path, dir, now := backupStatusEnv(t)
		unreadable(t, dir)
		got := ReadBackupStatus(probe(path, dir))
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
		got := ReadBackupStatus(probe(path, dir))
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
		got := ReadBackupStatus(probe(path, dir))
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

// probe is a BackupProbe one hour after the backup of backupStatusEnv.
func probe(dbPath, dir string) BackupProbe {
	return BackupProbe{DB: dbPath, Dir: dir, Loc: time.UTC, Now: time.Date(2026, 11, 10, 10, 30, 0, 0, time.UTC), Home: "/home/u"}
}

// TestAccessHint: the macOS privacy hint only for a directory in Documents,
// Desktop or Downloads of the home directory, naming that folder; otherwise
// the file permissions; "the backups are fine" only with a fresh backup
// proven by the status file.
func TestAccessHint(t *testing.T) {
	tests := []struct {
		name, goos, dir string
		fresh           bool
		want, not       []string
	}{
		{"documents on macOS", "darwin", "/home/u/Documents/total-recall", true, []string{"Documents", "Privacy & Security", "restart", "the backups are fine"}, []string{"permissions of"}},
		{"desktop", "darwin", "/home/u/Desktop/x", false, []string{"Desktop", "Privacy & Security", "restart"}, []string{"Documents", "fine"}},
		{"downloads", "darwin", "/home/u/Downloads/b", true, []string{"Downloads", "Privacy & Security"}, []string{"Documents"}},
		{"the folder itself", "darwin", "/home/u/Documents", false, []string{"Documents", "Privacy & Security"}, nil},
		{"unclean path", "darwin", "/home/u/Documents/../Documents/b/", false, []string{"Documents", "Privacy & Security"}, nil},
		{"elsewhere on macOS", "darwin", "/home/u/Library/Application Support/total-recall/backups", true, []string{"file permissions", "/home/u/Library/Application Support/total-recall/backups", "the backups are fine"}, []string{"Privacy", "Documents"}},
		{"similar name", "darwin", "/home/u/DocumentsOld/b", false, []string{"file permissions"}, []string{"Privacy", "fine"}},
		{"documents of another user", "darwin", "/home/other/Documents/b", false, []string{"file permissions"}, []string{"Privacy"}},
		{"documents on Linux", "linux", "/home/u/Documents/b", true, []string{"file permissions"}, []string{"Privacy", "restart"}},
		{"no home", "darwin", "/home/u/Documents/b", false, []string{"file permissions"}, []string{"Privacy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := "/home/u"
			if tt.name == "no home" {
				home = ""
			}
			got := accessHint(tt.dir, home, tt.goos, tt.fresh)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("hint %q lacks %q", got, w)
				}
			}
			for _, w := range tt.not {
				if strings.Contains(got, w) {
					t.Errorf("hint %q has %q", got, w)
				}
			}
		})
	}
}

// TestBackupHintFresh: in ReadBackupStatus "the backups are fine" needs a
// status file with a backup of the last 48 hours; without one, or with an
// old one, the hint does not claim it.
func TestBackupHintFresh(t *testing.T) {
	tests := []struct {
		name   string
		note   bool
		age    time.Duration // of the probe time after the backup
		isFine bool
	}{
		{"fresh status file", true, time.Hour, true},
		{"just under 48 hours", true, 47 * time.Hour, true},
		{"old status file", true, 5 * 24 * time.Hour, false},
		{"no status file", false, time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, path, dir, now := backupStatusEnv(t)
			if !tt.note {
				_ = os.Remove(BackupStatusPath(path))
			}
			unreadable(t, dir)
			p := probe(path, dir)
			p.Now = now.Add(tt.age)
			got := ReadBackupStatus(p)
			if fine := strings.Contains(got.Hint, "the backups are fine"); fine != tt.isFine {
				t.Errorf("hint %q: fine=%v, want %v", got.Hint, fine, tt.isFine)
			}
			if got.State != BackupUnknown || strings.Contains(got.Hint, "Privacy") {
				t.Errorf("temp dir is not under Documents: %+v", got)
			}
		})
	}
}
