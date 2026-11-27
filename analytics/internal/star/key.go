// Package star is the analytics schema of total-recall-analytics: a star
// schema (two facts on shared dimensions, marts on top, versioned views as
// the interface) built in DuckDB from total-recall archives.
//
// The models are plain SQL files in project/ in the subset of dbt the
// runner understands (runner.go); Go drives a run (etl.go): it attaches the
// archive, chooses the events to load (watermark per source), computes the
// values that come from the configuration (project and login names, as
// seeds) and checks the contract of the views and the data tests.
package star

import (
	"crypto/md5" //nolint:gosec // a surrogate key, not a security boundary
	"encoding/binary"
	"strings"
)

// keySep separates the parts of a natural key before hashing (ASCII unit
// separator, never part of a name or path).
const keySep = "\x1f"

// Key is the surrogate key of a dimension member or fact row, the Go twin
// of the SQL macro tr_key (macros.sql): the first 8 bytes of the MD5 of the
// parts joined by U+001F, big-endian, as a signed integer; 0 maps to 1. The
// first part names the dimension.
func Key(parts ...string) int64 {
	sum := md5.Sum([]byte(strings.Join(parts, keySep))) //nolint:gosec // see above
	k := int64(binary.BigEndian.Uint64(sum[:8]))        //nolint:gosec // wrap-around intended
	if k == 0 {
		return 1
	}
	return k
}

// Tilde replaces the home directory at the start of path with "~", the Go
// twin of the SQL macro tr_tilde.
func Tilde(path, home string) string {
	h := strings.TrimSuffix(home, "/")
	switch {
	case home == "" || path == "":
		return path
	case path == home || path == h:
		return "~"
	case strings.HasPrefix(path, h+"/"):
		return "~" + path[len(h):]
	}
	return path
}

// untilde reverses Tilde.
func untilde(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return strings.TrimSuffix(home, "/") + path[1:]
	}
	return path
}
