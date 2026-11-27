//go:build linux && amd64

package duck

import _ "embed"

// Platform is DuckDB's name for the platform this binary is built for.
const Platform = "linux_amd64"

//go:embed ext/linux_amd64/sqlite_scanner.duckdb_extension.gz
var sqliteExtGz []byte
