package cli

import (
	"fmt"
	"io"
	"path/filepath"
)

// syncDomain returns the directory that puts dir into a file-provider
// (iCloud Drive) domain: dir itself or its nearest parent for which has
// reports the domain marker; "" when there is none. A directory that does
// not exist yet counts through its parents.
func syncDomain(dir string, has func(string) bool) string {
	for p := filepath.Clean(dir); ; {
		if has(p) {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}

// DoctorBackupHintJSON is the hint of doctor --json about a backup
// directory that lies in a file-provider (iCloud) domain.
type DoctorBackupHintJSON struct {
	Dir       string `json:"dir"`
	DomainDir string `json:"domain_dir"` // the directory that carries the domain
	Hint      string `json:"hint"`
}

// backupSyncHint returns the hint for the backup directory dir, nil when it
// is outside every file-provider domain.
func backupSyncHint(dir string, has func(string) bool) *DoctorBackupHintJSON {
	dom := syncDomain(dir, has)
	if dom == "" {
		return nil
	}
	return &DoctorBackupHintJSON{Dir: dir, DomainDir: dom, Hint: fmt.Sprintf(
		"the backup directory %s is in an iCloud / file provider domain (%s): every backup is synced, which costs iCloud storage and bandwidth; consider a [backup] dir outside, e.g. ~/Library/Application Support/total-recall/backups (the default is not changed)",
		dir, dom)}
}

// writeBackupSyncHint prints the hint like the other doctor hints.
func writeBackupSyncHint(w io.Writer, h *DoctorBackupHintJSON) {
	_, _ = fmt.Fprintf(w, "hint: %s\n\n", h.Hint)
}
