package ops

import "github.com/seletz/total-recall/internal/store"

// HostCell describes the host of the archive for status and the status
// page of the TUI: the configured host, then a note on what the archive
// recorded. The note is a warning (warn) when the archive does not name
// the configured host yet.
func HostCell(host string, a store.Archive) (value, note string, warn bool) {
	value = host
	if value == "" {
		value = "unknown"
	}
	switch {
	case a.ID == "":
		return value, "the archive records it after migrate", true
	case host == "" && a.Host == "":
		return value, "archive " + a.ID + ", no host: set host in the config", true
	case host == "":
		return a.Host, "archive " + a.ID + ", host unknown on this machine: set host in the config", true
	case a.Host == "":
		return value, "archive " + a.ID + ", recorded at the next write", true
	case a.Host != host:
		return value, "archive " + a.ID + ", recorded as " + a.Host + " until the next write", true
	}
	return value, "archive " + a.ID, false
}
