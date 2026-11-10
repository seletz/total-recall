package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
		{"EACCES in Documents is a plain permission problem", "darwin", "/home/u/Documents/b", true, []string{"file permissions", "the backups are fine"}, []string{"Privacy", "restart"}},
		{"no home", "darwin", "/home/u/Documents/b", false, []string{"file permissions"}, []string{"Privacy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := "/home/u"
			if tt.name == "no home" {
				home = ""
			}
			got := accessHint(tt.dir, home, tt.goos, tt.fresh, !strings.HasPrefix(tt.name, "EACCES"))
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

// TestBackupStatusErrors: whatever the read of the directory fails with
// (EPERM, EACCES, EIO, ...), in any folder, the state is unknown with
// reason and hint; only a missing or an empty directory is none. The macOS
// privacy hint needs EPERM (what the protection reports) in Documents,
// Desktop or Downloads of the home directory.
func TestBackupStatusErrors(t *testing.T) {
	errs := []struct {
		name    string
		err     error
		unknown bool
		reason  string
	}{
		{"EPERM", &fs.PathError{Op: "open", Path: "x", Err: syscall.EPERM}, true, "operation not permitted"},
		{"EACCES", &fs.PathError{Op: "open", Path: "x", Err: syscall.EACCES}, true, "permission denied"},
		{"EIO", &fs.PathError{Op: "open", Path: "x", Err: syscall.EIO}, true, "input/output error"},
		{"ENOTDIR", &fs.PathError{Op: "open", Path: "x", Err: syscall.ENOTDIR}, true, "not a directory"},
		{"other error", errors.New("boom"), true, "boom"},
		{"ENOENT", &fs.PathError{Op: "open", Path: "x", Err: syscall.ENOENT}, false, ""},
		{"empty directory", nil, false, ""},
	}
	folders := []struct {
		dir string
		tcc bool
	}{
		{"/home/u/Documents/total-recall", true},
		{"/home/u/Desktop/b", true},
		{"/home/u/Downloads/b", true},
		{"/home/u/Library/Application Support/total-recall/backups", false},
		{"/var/backups/total-recall", false},
	}
	for _, e := range errs {
		for _, f := range folders {
			t.Run(e.name+" in "+f.dir, func(t *testing.T) {
				readDir := func(string) ([]os.DirEntry, error) { return nil, e.err }
				got := readBackupStatus(BackupProbe{DB: filepath.Join(t.TempDir(), "a.db"), Dir: f.dir, Loc: time.UTC, Home: "/home/u"}, readDir, func(string) error { return nil }, "darwin")
				if !e.unknown {
					if got.State != BackupNone || got.Reason != "" || got.Hint != "" {
						t.Errorf("want none: %+v", got)
					}
					return
				}
				if got.State != BackupUnknown || got.Reason != "no access: "+e.reason || got.Hint == "" {
					t.Fatalf("want unknown / %q: %+v", e.reason, got)
				}
				privacy := strings.Contains(got.Hint, "Privacy & Security")
				if want := f.tcc && e.name == "EPERM"; privacy != want {
					t.Errorf("privacy hint = %v, want %v: %q", privacy, want, got.Hint)
				}
				if !privacy && !strings.Contains(got.Hint, "file permissions of "+f.dir) {
					t.Errorf("hint %q", got.Hint)
				}
			})
		}
	}
}

// TestBackupStatusExistsButClosed: a directory that exists but cannot be
// used is unknown even when the listing itself says nothing is wrong (an
// empty list, or "does not exist" as some file systems report it): the
// access check of the directory decides. Only a directory that is really
// missing, or readable and empty, is none.
func TestBackupStatusExistsButClosed(t *testing.T) {
	eacces := &fs.PathError{Op: "access", Path: "x", Err: syscall.EACCES}
	notExist := &fs.PathError{Op: "open", Path: "x", Err: syscall.ENOENT}
	tests := []struct {
		name    string
		listErr error
		access  error
		want    BackupState
	}{
		{"empty list, access denied", nil, eacces, BackupUnknown},
		{"does not exist, but access says denied", notExist, eacces, BackupUnknown},
		{"empty list, access ok", nil, nil, BackupNone},
		{"does not exist, access ok (really missing)", notExist, nil, BackupNone},
		{"listing fails, access ok", &fs.PathError{Op: "open", Path: "x", Err: syscall.EPERM}, nil, BackupUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readDir := func(string) ([]os.DirEntry, error) { return nil, tt.listErr }
			access := func(string) error { return tt.access }
			got := readBackupStatus(BackupProbe{DB: filepath.Join(t.TempDir(), "a.db"), Dir: "/home/u/Documents/b", Loc: time.UTC, Home: "/home/u"}, readDir, access, "darwin")
			if got.State != tt.want {
				t.Fatalf("state %s, want %s: %+v", got.State, tt.want, got)
			}
			if tt.want == BackupUnknown && (got.Reason == "" || got.Hint == "") {
				t.Errorf("no reason or hint: %+v", got)
			}
			if tt.want == BackupNone && (got.Reason != "" || got.Hint != "") {
				t.Errorf("reason or hint with none: %+v", got)
			}
		})
	}
}

// TestDirAccessError: the real check on real directories: missing is fine
// (nil), a readable one is nil, one without the read or the search bit is an
// error, and so is a path below a closed directory.
func TestDirAccessError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root passes every access check")
	}
	root := t.TempDir()
	mk := func(name string, mode os.FileMode) string {
		p := filepath.Join(root, name)
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
		return p
	}
	closed := mk("closed", 0)
	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{"readable", mk("open", 0o755), false},
		{"missing", filepath.Join(root, "missing"), false},
		{"mode 000", closed, true},
		{"no read bit", mk("noread", 0o311), true},
		{"no search bit", mk("nosearch", 0o444), true},
		{"below a closed directory", filepath.Join(closed, "sub"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := dirAccessError(tt.dir); (err != nil) != tt.wantErr {
				t.Errorf("dirAccessError = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}
