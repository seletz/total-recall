//go:build unix

package store

import "syscall"

// accessDir asks the system whether we may list (read) and enter (search)
// dir: access(2) with R_OK and X_OK, which also covers ACLs.
func accessDir(dir string) error {
	return syscall.Access(dir, 0x4|0x1) // R_OK|X_OK
}
