package config

import (
	"regexp"
	"strings"
)

// hostPattern is a valid host: usable as a file or directory name in a
// pool directory on every platform.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// HostDefault derives the default of the key host from the machine's host
// name: its first label (before the first dot) in lower case; "" when the
// name is unknown or not a valid host.
func HostDefault(hostname func() (string, error)) string {
	n, err := hostname()
	if err != nil {
		return ""
	}
	n, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(n)), ".")
	if !hostPattern.MatchString(n) {
		return ""
	}
	return n
}
