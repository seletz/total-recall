//go:build linux && arm64

package duck

import _ "embed"

// Platform is DuckDB's name for the platform this binary is built for.
const Platform = "linux_arm64"

//go:embed ext/linux_arm64/sqlite_scanner.duckdb_extension.gz
var sqliteExtGz []byte
