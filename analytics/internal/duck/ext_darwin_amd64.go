//go:build darwin && amd64

package duck

import _ "embed"

// Platform is DuckDB's name for the platform this binary is built for.
const Platform = "osx_amd64"

//go:embed ext/osx_amd64/sqlite_scanner.duckdb_extension.gz
var sqliteExtGz []byte
