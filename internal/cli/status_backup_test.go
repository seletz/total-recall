package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatusBackupState: status says ok with a backup, none for an empty
// directory and unknown, with reason and hint, when the directory cannot be
// read (the privacy block of macOS looks like a permission error); then the
// status file next to the database still gives the last backup. The new
// JSON fields are additive: last_backup and last_backup_at stay.
func TestStatusBackupState(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, backup bool) *testEnv {
		e := newTestEnv(t)
		e.mustRun(t, "ingest", "--all")
		if err := os.MkdirAll(e.backups, 0o755); err != nil {
			t.Fatal(err)
		}
		if backup {
			e.mustRun(t, "backup")
		}
		return e
	}
	lock := func(t *testing.T, e *testEnv) {
		if os.Geteuid() == 0 {
			t.Skip("root reads every directory")
		}
		if err := os.Chmod(e.backups, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(e.backups, 0o755) })
	}
	tests := []struct {
		name           string
		backup, locked bool
		noNote         bool
		state          string
		path, reason   bool // last_backup set; last_backup_reason set
		text           []string
		notText        []string
	}{
		{name: "readable with a backup", backup: true, state: "ok", path: true, text: []string{"last backup", "archive-2026-10-20.db.zst"}, notText: []string{"unknown"}},
		{name: "empty directory", state: "none", text: []string{"none in " + "BACKUPS"}, notText: []string{"unknown"}},
		{name: "unreadable with status file", backup: true, locked: true, state: "unknown", path: true, reason: true,
			text: []string{"unknown", "no access: ", "archive-2026-10-20.db.zst", "hint:"}, notText: []string{"none in"}},
		{name: "unreadable without status file", backup: true, locked: true, noNote: true, state: "unknown", reason: true,
			text: []string{"unknown", "no access: ", "hint:"}, notText: []string{"none in"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t, tt.backup)
			if tt.noNote {
				_ = os.Remove(e.db + ".backup.json")
			}
			if tt.locked {
				lock(t, e)
			}
			s := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
			if s.LastBackupState != tt.state {
				t.Errorf("last_backup_state = %q, want %q", s.LastBackupState, tt.state)
			}
			if (s.LastBackup != "") != tt.path || (s.LastBackupAt != "") != tt.path {
				t.Errorf("last_backup %q, last_backup_at %q, want set=%v", s.LastBackup, s.LastBackupAt, tt.path)
			}
			if (s.LastBackupReason != "") != tt.reason || (s.LastBackupHint != "") != tt.reason {
				t.Errorf("reason %q hint %q, want set=%v", s.LastBackupReason, s.LastBackupHint, tt.reason)
			}
			out := e.mustRun(t, "status")
			for _, w := range tt.text {
				if !strings.Contains(out, strings.ReplaceAll(w, "BACKUPS", e.backups)) {
					t.Errorf("status lacks %q:\n%s", w, out)
				}
			}
			for _, w := range tt.notText {
				if strings.Contains(out, w) {
					t.Errorf("status has %q:\n%s", w, out)
				}
			}
		})
	}
}

// TestStatusBackupUnderDocuments: the PA's case end to end: a backup
// directory below <home>/Documents given with --backup-dir, closed with
// chmod 000, is unknown with reason and the file-permissions hint (EACCES is
// no privacy block), as it is anywhere else; it is never none without a
// reason.
func TestStatusBackupUnderDocuments(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	for _, where := range []string{"Documents", "Desktop", "Downloads", "elsewhere"} {
		t.Run(where, func(t *testing.T) {
			e := newTestEnv(t)
			e.mustRun(t, "ingest", "--all")
			e.home = e.dir
			dir := filepath.Join(e.dir, where, "tr-test-nobak")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			s := decode[StatusJSON](t, e.mustRun(t, "--backup-dir", dir, "status", "--json"))
			if s.BackupDir != dir || s.LastBackupState != "unknown" || s.LastBackupReason == "" || s.LastBackupHint == "" {
				t.Fatalf("backup_dir %q state %q reason %q hint %q", s.BackupDir, s.LastBackupState, s.LastBackupReason, s.LastBackupHint)
			}
			if !strings.Contains(string(s.LastBackupHint), "file permissions of "+dir) || strings.Contains(string(s.LastBackupHint), "Privacy") {
				t.Errorf("hint %q", s.LastBackupHint)
			}
		})
	}
}
