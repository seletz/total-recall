//go:build darwin

package cli

import "golang.org/x/sys/unix"

// fileProviderMarker is the extended attribute the file provider framework
// (iCloud Drive) sets on the root directory of a domain.
const fileProviderMarker = "com.apple.file-provider-domain-id"

// hasFileProviderDomain reports whether path carries the domain marker: a
// plain getxattr, no cgo.
func hasFileProviderDomain(path string) bool {
	_, err := unix.Getxattr(path, fileProviderMarker, nil)
	return err == nil
}
