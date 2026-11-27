// Package analytics is the prototype of spike #349: a star schema (two
// facts on shared dimensions, marts on top, versioned read-only views as
// the interface) and an ETL that fills it from an archive. The SQL lives
// in sql/*.sql, the data checks in checks/*.sql; Go drives the steps,
// supplies the deterministic keys and the values that come from the
// configuration (project and login names) and the time-zone bridge.
//
// It is a spike: no CLI command uses it; tools/analytics runs it.
package analytics

import (
	"crypto/md5" //nolint:gosec // a surrogate key, not a security boundary
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"

	"modernc.org/sqlite"
)

// keySep separates the parts of a natural key before hashing (ASCII unit
// separator, never part of a name or path).
const keySep = "\x1f"

// Key is the surrogate key of a dimension member or fact row: the first 8
// bytes of the MD5 of the parts joined by U+001F, big-endian, as a signed
// integer. The first part names the dimension, so equal natural keys of
// different dimensions differ. The same expression in PostgreSQL is
//
//	('x' || substr(md5(concat_ws(chr(31), parts...)), 1, 16))::bit(64)::bigint
//
// so a load into either database yields the same keys. 0 is reserved and
// maps to 1.
func Key(parts ...string) int64 {
	sum := md5.Sum([]byte(strings.Join(parts, keySep))) //nolint:gosec // see above
	k := int64(binary.BigEndian.Uint64(sum[:8]))        //nolint:gosec // wrap-around intended
	if k == 0 {
		return 1
	}
	return k
}

// Tilde replaces the home directory at the start of path with "~", so the
// same project on hosts with different home directories gets one key.
func Tilde(path, home string) string {
	switch {
	case home == "" || path == "":
		return path
	case path == home:
		return "~"
	case strings.HasPrefix(path, strings.TrimSuffix(home, "/")+"/"):
		return "~" + path[len(strings.TrimSuffix(home, "/")):]
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

var registerOnce sync.Once

// register makes tr_key and tr_tilde available to every SQLite connection
// opened afterwards. The driver keeps registered functions process-wide,
// so this runs once.
func register() {
	registerOnce.Do(func() {
		sqlite.MustRegisterDeterministicScalarFunction("tr_key", -1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			parts := make([]string, len(args))
			for i, a := range args {
				parts[i] = valueString(a)
			}
			return Key(parts...), nil
		})
		sqlite.MustRegisterDeterministicScalarFunction("tr_tilde", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			return Tilde(valueString(args[0]), valueString(args[1])), nil
		})
	})
}

// valueString renders an SQL value as the text of a natural key; NULL is "".
func valueString(v driver.Value) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
