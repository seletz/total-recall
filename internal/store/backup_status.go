package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// BackupState says what is known about the last backup.
type BackupState string

// The states of BackupStatus. none means the directory was read and holds
// no backup; unknown that it could not be read (on macOS the privacy
// protection of ~/Documents does this to a process without the permission),
// which says nothing about the backups.
const (
	BackupOK      BackupState = "ok"
	BackupNone    BackupState = "none"
	BackupUnknown BackupState = "unknown"
)

// BackupStatus is the answer of ReadBackupStatus.
type BackupStatus struct {
	State BackupState
	// Path, At and Size describe the newest copy. Set with ok; with unknown
	// only when the status file next to the database has them.
	Path string
	At   time.Time
	Size int64
	// Reason and Hint are set with unknown: why the directory cannot be
	// read, and what to do about it.
	Reason, Hint string
}

// backupNote is the small status file a backup writes next to the database
// after every run: when, which copy and how big, so the last backup is known
// even where the backup directory cannot be read.
type backupNote struct {
	Time time.Time `json:"time"`
	File string    `json:"file"`
	Size int64     `json:"size"`
}

// BackupStatusPath is the status file of the database at dbPath.
func BackupStatusPath(dbPath string) string { return dbPath + ".backup.json" }

// writeBackupNote records the copy at path, made at when, next to the
// database; the file is replaced atomically.
func writeBackupNote(dbPath, copyPath string, when time.Time) error {
	fi, err := os.Stat(copyPath)
	if err != nil {
		return err
	}
	data, err := json.Marshal(backupNote{Time: when.UTC(), File: copyPath, Size: fi.Size()})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dbPath), "backup-status-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, BackupStatusPath(dbPath))
}

// readBackupNote reads the status file of the database; false if it is
// missing or damaged.
func readBackupNote(dbPath string) (backupNote, bool) {
	data, err := os.ReadFile(BackupStatusPath(dbPath))
	if err != nil {
		return backupNote{}, false
	}
	var n backupNote
	if json.Unmarshal(data, &n) != nil || n.Time.IsZero() || n.File == "" {
		return backupNote{}, false
	}
	return n, true
}

// ReadBackupStatus returns the state of the last backup in dir: ok with the
// newest copy, none when dir holds no copy (or does not exist), and unknown
// when dir cannot be read, with the reason and a hint. Then the status file
// next to the database at dbPath, if there is one, still gives the time, the
// copy and its size of the last backup run. A readable directory is always
// the truth: a status file does not make a deleted backup exist.
func ReadBackupStatus(dbPath, dir string, loc *time.Location) BackupStatus {
	path, at, err := LastBackup(dir, loc)
	if err != nil {
		st := BackupStatus{State: BackupUnknown, Reason: "no access: " + reasonOf(err), Hint: accessHint(dir)}
		if n, ok := readBackupNote(dbPath); ok {
			st.Path, st.At, st.Size = n.File, n.Time.In(loc), n.Size
		}
		return st
	}
	if path == "" {
		return BackupStatus{State: BackupNone}
	}
	st := BackupStatus{State: BackupOK, Path: path, At: at}
	if fi, err := os.Stat(path); err == nil {
		st.Size = fi.Size()
	}
	return st
}

// reasonOf is the plain cause of err: for a failed path operation the
// system error without the path ("permission denied").
func reasonOf(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// accessHint says what to do when the backup directory cannot be read.
func accessHint(_ string) string {
	if runtime.GOOS == "darwin" {
		return "allow this app access to Documents (Privacy & Security > Files & Folders), then restart the terminal or app (running processes keep the old rights), or set [backup] dir elsewhere; the backups are fine"
	}
	return "check the permissions of the backup directory; the backups are fine"
}

// EnsureBackupNote writes the status file for the copy at path (made at
// at) unless the database has a valid one: a skipped backup run (the newest
// copy is young enough) thus gives the status file to an archive whose
// backups predate it.
func EnsureBackupNote(dbPath, path string, at time.Time) error {
	if _, ok := readBackupNote(dbPath); ok {
		return nil
	}
	return writeBackupNote(dbPath, path, at)
}
