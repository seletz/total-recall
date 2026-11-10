package store

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// dirAccessError checks that dir, when it exists, can really be used: it
// returns nil for a directory that is missing (that is "none", not a
// problem) and for one that can be listed, and the error otherwise. It does
// not trust the listing alone: a directory that exists but is closed to us
// (no read or search permission, a privacy block, a file provider) must not
// pass for an empty one, whatever the listing reported.
func dirAccessError(dir string) error {
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return nil
	}
	if err := accessDir(dir); err != nil {
		return &fs.PathError{Op: "access", Path: dir, Err: err}
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
