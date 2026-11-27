//go:build duckdb_use_static_lib

package duck

// The DuckDB of scripts/analytics-duckdb.sh, with sqlite_scanner and the
// other extensions compiled in; libduckdb_static last.

/*
#cgo LDFLAGS: -L${SRCDIR}/../../duckdb/lib/linux_amd64 -lduckdb_static_extension_loader -lsqlite_scanner_extension -lparquet_extension -ljson_extension -licu_extension -lcore_functions_extension -lduckdb_static -lstdc++ -lm -ldl
*/
import "C"
