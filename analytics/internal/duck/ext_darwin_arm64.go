//go:build darwin && arm64

package duck

import _ "embed"

// Platform is DuckDB's name for the platform this binary is built for.
const Platform = "osx_arm64"

//go:embed ext/osx_arm64/sqlite_scanner.duckdb_extension.gz
var sqliteExtGz []byte
