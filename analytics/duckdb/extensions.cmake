# Extensions compiled into the DuckDB of total-recall-analytics
# (scripts/analytics-duckdb.sh). Nothing is loaded at run time: the build has
# no loadable_extensions capability, so LOAD and INSTALL fail.
#
# core_functions, icu (time zones), json and parquet (export) are part of the
# DuckDB repository. sqlite_scanner reads the total-recall archives; it lives
# in its own repository, pinned to the commit DuckDB itself pins for this
# release (.github/config/extensions/sqlite_scanner.cmake in DuckDB).
duckdb_extension_load(core_functions)
duckdb_extension_load(icu)
duckdb_extension_load(json)
duckdb_extension_load(parquet)
duckdb_extension_load(sqlite_scanner
        GIT_URL https://github.com/duckdb/duckdb-sqlite
        GIT_TAG ce0987fda4f6393231c6783ee32f72ab873a6d3e
        SUBMODULES database-connector
        )
