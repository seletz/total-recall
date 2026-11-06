package claude

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/seletz/total-recall/internal/blobref"
)

// File history (#183). Before it edits a file, Claude Code copies it to
// $CLAUDE_CONFIG_DIR/file-history/<session uuid>/<backupFileName>
// (backupFileName is "<hash of the path>@v<N>") and notes the copy in the
// transcript: file-history-snapshot carries the index of all copies so
// far (snapshot.trackedFileBackups, keyed by the file's path, repeated
// and extended every turn), file-history-delta one new copy (backup).
// The copies are deleted with the transcript, so the adapter reads them
// on ingest: the first record of a Parse call that names a copy gets its
// content as an attachment (text/plain, or application/octet-stream for
// binary data) and its object in the line gets the member
// "blob":"blob:sha256:<hex>" (blobref.Annotate); later records that
// repeat the name stay as they are, the store keeps the content once per
// hash. A copy that is gone leaves the record as it is (counted, logged
// once per transcript); one over the size cap is logged and left out.

// FileHistoryBlobKey is the member the adapter adds to a backup object of
// a file-history record: the reference to the archived content.
const FileHistoryBlobKey = "blob"

// DefaultFileHistoryMaxBytes is the size above which a backup file is
// not archived (WithFileHistoryMaxBytes).
const DefaultFileHistoryMaxBytes = 16 << 20

// fileHistoryTypes maps the record types that name backup files to the
// JSON path (prefix) of their backup objects.
var fileHistoryTypes = map[string]string{
	"file-history-snapshot": "snapshot.trackedFileBackups.",
	"file-history-delta":    "backup",
}

// DefaultFileHistoryDir returns $CLAUDE_CONFIG_DIR/file-history, or
// ~/.claude/file-history.
func DefaultFileHistoryDir() (string, error) {
	d, err := DefaultProjectsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(d), "file-history"), nil
}

// WithFileHistoryDir makes the adapter archive the backup copies of
// edited files below dir (see DefaultFileHistoryDir). Empty, the default,
// leaves the file-history records as they are.
func WithFileHistoryDir(dir string) Option {
	return func(a *Adapter) { a.fileHistory = dir }
}

// WithFileHistoryMaxBytes caps the size of a backup file that is
// archived (default DefaultFileHistoryMaxBytes); n <= 0 keeps the default.
func WithFileHistoryMaxBytes(n int64) Option {
	return func(a *Adapter) {
		if n > 0 {
			a.fileHistoryMax = n
		}
	}
}

// WithLogf sets where the adapter writes diagnostics (backup files it
// could not archive); default discard.
func WithLogf(f func(format string, args ...any)) Option {
	return func(a *Adapter) {
		if f != nil {
			a.logf = f
		}
	}
}

// fileHistory annotates the backup objects of a file-history record whose
// copies are on disk and returns the line and the attachments; a record
// of another type, or one without copies to add, comes back as it is.
func (p *parser) fileHistory(typ string, line []byte) ([]byte, []blobref.Attachment) {
	prefix, ok := fileHistoryTypes[typ]
	if !ok || p.a.fileHistory == "" || !bytes.Contains(line, []byte("backupFileName")) {
		return line, nil
	}
	out, atts, err := blobref.Annotate(line, func(path string, f blobref.Fields) (string, string, []byte, bool) {
		if path != prefix && !strings.HasPrefix(path, prefix) {
			return "", "", nil, false
		}
		name := f["backupFileName"]
		if name == "" || f[FileHistoryBlobKey] != "" || !validBackupName(name) || p.backups[name] {
			return "", "", nil, false
		}
		p.backups[name] = true
		data, media, ok := p.readBackup(name)
		if !ok {
			return "", "", nil, false
		}
		return FileHistoryBlobKey, media, data, true
	})
	if err != nil {
		return line, nil
	}
	return out, atts
}

// validBackupName accepts a file name without path elements.
func validBackupName(name string) bool {
	return name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

// readBackup reads one backup file of the transcript's session; ok is
// false (and the counters updated) when it is missing, over the cap or
// unreadable.
func (p *parser) readBackup(name string) (data []byte, mediaType string, ok bool) {
	path := filepath.Join(p.a.fileHistory, p.file.base, name)
	fi, err := os.Stat(path)
	if err != nil {
		p.missing++
		return nil, "", false
	}
	if fi.Size() > p.a.fileHistoryMax {
		p.a.logf("claude: file history: %s not archived: %d bytes over the cap of %d", path, fi.Size(), p.a.fileHistoryMax)
		p.oversized++
		return nil, "", false
	}
	data, err = os.ReadFile(path)
	if err != nil {
		p.a.logf("claude: file history: %s not archived: %v", path, err)
		p.missing++
		return nil, "", false
	}
	p.a.countBackup(int64(len(data)))
	if isText(data) {
		return data, "text/plain", true
	}
	return data, "application/octet-stream", true
}

// isText reports whether data looks like text: valid UTF-8 without NUL
// bytes. Text is redacted by ingest, binary data is kept as it is.
func isText(data []byte) bool {
	return !bytes.ContainsRune(data, 0) && utf8.Valid(data)
}

// finishFileHistory logs the backups of the transcript that were gone,
// once per Parse call, and moves the counters to the adapter's stats.
func (p *parser) finishFileHistory() {
	if p.missing > 0 {
		p.a.logf("claude: file history: %s: %d backup files no longer on disk, references kept", p.path, p.missing)
	}
	p.a.mu.Lock()
	p.a.stats.BackupsMissing += p.missing
	p.a.stats.BackupsOversized += p.oversized
	p.a.mu.Unlock()
	p.missing, p.oversized = 0, 0
}

func (a *Adapter) countBackup(n int64) {
	a.mu.Lock()
	a.stats.Backups++
	a.stats.BackupBytes += n
	a.mu.Unlock()
}
