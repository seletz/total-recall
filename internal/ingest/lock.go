package ingest

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockFile takes a non-blocking exclusive flock on a lock file for source in
// dir (named after the SHA-1 of the source path). ok is false if another
// process holds it; the caller then skips the source instead of waiting.
func lockFile(dir, source string) (unlock func(), ok bool, err error) {
	if dir == "" {
		return nil, false, fmt.Errorf("ingest: no lock directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, fmt.Errorf("ingest: lock dir: %w", err)
	}
	h := sha1.Sum([]byte(source))
	f, err := os.OpenFile(filepath.Join(dir, hex.EncodeToString(h[:])+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("ingest: lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("ingest: lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}
