//go:build !unix

package store

// accessDir has no access(2) here; opening the directory decides.
func accessDir(string) error { return nil }
