//go:build !darwin

package cli

// hasFileProviderDomain is false: only macOS has file-provider domains.
func hasFileProviderDomain(string) bool { return false }
