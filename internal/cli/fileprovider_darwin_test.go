//go:build darwin

package cli

import "testing"

// TestHasFileProviderDomain: the real xattr query works without cgo and
// finds no marker on a plain temp directory or a missing path.
func TestHasFileProviderDomain(t *testing.T) {
	if hasFileProviderDomain(t.TempDir()) {
		t.Error("a temp directory has no file-provider marker")
	}
	if hasFileProviderDomain("/nonexistent/path") {
		t.Error("a missing path has no marker")
	}
}
