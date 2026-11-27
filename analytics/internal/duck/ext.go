package duck

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed ext.lock
var lockFile []byte

// Lock is the parsed ext.lock: the DuckDB version and the checksums of the
// embedded extensions per platform.
type Lock struct {
	DuckDB  string
	Entries []LockEntry
}

// LockEntry pins one extension for one platform.
type LockEntry struct {
	Name     string
	Platform string
	// GzSHA256 is the checksum of the .gz as downloaded and embedded.
	GzSHA256 string
	// SHA256 is the checksum of the extension DuckDB loads.
	SHA256 string
}

// Find returns the entry for name and platform.
func (l Lock) Find(name, platform string) (LockEntry, bool) {
	for _, e := range l.Entries {
		if e.Name == name && e.Platform == platform {
			return e, true
		}
	}
	return LockEntry{}, false
}

// ParseLock parses an ext.lock: comment lines, one "duckdb <version>" line
// and lines "<name> <platform> <gz sha256> <sha256>".
func ParseLock(b []byte) (Lock, error) {
	var l Lock
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch {
		case f[0] == "duckdb" && len(f) == 2:
			l.DuckDB = f[1]
		case f[0] == SQLiteExtension && len(f) == 4:
			l.Entries = append(l.Entries, LockEntry{Name: f[0], Platform: f[1], GzSHA256: f[2], SHA256: f[3]})
		default:
			return l, fmt.Errorf("ext.lock line %d: %q", n, line)
		}
	}
	if l.DuckDB == "" {
		return l, fmt.Errorf("ext.lock: no duckdb version")
	}
	return l, sc.Err()
}

// materialize writes the embedded extension to dir unless a file with the
// pinned checksum is already there, and returns its path. The file is written
// to a temporary name and renamed, so concurrent runs never see half a file.
func materialize(dir string, e LockEntry) (string, error) {
	path := filepath.Join(dir, e.Name+".duckdb_extension")
	if fileSHA256(path) == e.SHA256 {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("extension directory: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(sqliteExtGz))
	if err != nil {
		return "", fmt.Errorf("embedded extension: %w", err)
	}
	tmp, err := os.CreateTemp(dir, e.Name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("extension directory: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), zr); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write extension: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("write extension: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != e.SHA256 {
		return "", fmt.Errorf("embedded %s for %s has sha256 %s, ext.lock pins %s", e.Name, e.Platform, got, e.SHA256)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("write extension: %w", err)
	}
	return path, nil
}

// fileSHA256 is the hex checksum of the file at path, "" if unreadable.
func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
