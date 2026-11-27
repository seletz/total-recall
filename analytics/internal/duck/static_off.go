//go:build !duckdb_use_static_lib

package duck

// staticBuild is false: without the build tag duckdb-go links its prebuilt
// DuckDB, which lacks the sqlite extension; Open refuses to run.
const staticBuild = false
