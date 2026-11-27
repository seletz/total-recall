#!/usr/bin/env bash
# Spike #349: time the mart computations (tools/analytics/duckdb/bench.sql)
# on SQLite and DuckDB for the demo archive scaled by the factors given
# (default 1 10 100). Synthetic data only; everything goes to $OUT
# (default build/bench). Needs sqlite3 and duckdb on PATH.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=${OUT:-build/bench}
mkdir -p "$OUT"
[ -f build/demo.db ] || go run ./tools/demo -out build/demo.db
go build -o "$OUT/tr-analytics" ./tools/analytics
bench=tools/analytics/duckdb/bench.sql
ms() { python3 -c 'import sys,time; print(int(time.time()*1000))'; }
for f in "${@:-1 10 100}"; do
  for factor in $f; do
    arch="$OUT/archive-x$factor.db" an="$OUT/analytics-x$factor.db" duck="$OUT/analytics-x$factor.duckdb"
    rm -f "$arch" "$an" "$an-wal" "$an-shm" "$duck"
    sqlite3 build/demo.db "VACUUM INTO '$arch'"
    if [ "$factor" -gt 1 ]; then
      sqlite3 "$arch" "
        WITH RECURSIVE k(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM k WHERE n < $factor - 1)
        INSERT INTO sessions (id, harness, harness_version, parent_id, cwd, git_root, git_remote_url, git_branch, model, started_at, ended_at, account_id, organization_id)
        SELECT s.id || '-' || n, harness, harness_version, CASE WHEN parent_id IS NOT NULL THEN parent_id || '-' || n END, cwd, git_root, git_remote_url, git_branch, model, started_at, ended_at, account_id, organization_id FROM sessions s, k;
        WITH RECURSIVE k(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM k WHERE n < $factor - 1)
        INSERT INTO events (seq, id, session_id, ts, ts_local, kind, role, actor, tool_name, model, tokens_in, tokens_out, source_id, source_offset, tokens_cache_read, tokens_cache_write, input_kind)
        SELECT seq + n * 100000, id || '-' || n, session_id || '-' || n, ts, ts_local, kind, role, actor, tool_name, model, tokens_in, tokens_out, source_id, source_offset, tokens_cache_read, tokens_cache_write, input_kind FROM events e, k WHERE e.seq <= 100000;"
    fi
    events=$(sqlite3 "$arch" "SELECT COUNT(*) FROM events")
    t0=$(ms); etl=$("$OUT/tr-analytics" -archive "$arch" -out "$an" -source "x$factor"); t1=$(ms)
    echo "x$factor: $events events; ETL (SQLite, pure Go driver): $((t1 - t0)) ms; $etl"
    t0=$(ms); sqlite3 "$an" < "$bench" > "$OUT/sqlite-x$factor.out"; t1=$(ms)
    echo "x$factor: bench SQLite (sqlite3 CLI):          $((t1 - t0)) ms"
    t0=$(ms); { echo "LOAD sqlite; ATTACH '$an' AS a (TYPE sqlite, READ_ONLY); USE a;"; cat "$bench"; } | duckdb -no-agent -init /dev/null -csv > "$OUT/duck-scan-x$factor.out"; t1=$(ms)
    echo "x$factor: bench DuckDB over the SQLite file:  $((t1 - t0)) ms"
    t0=$(ms); duckdb -no-agent -init /dev/null "$duck" -c "LOAD sqlite; ATTACH '$an' AS a (TYPE sqlite, READ_ONLY); $(for t in fact_event bridge_time_slot dim_host dim_project dim_actor dim_event_type dim_tool; do printf 'CREATE TABLE %s AS SELECT * FROM a.%s; ' $t $t; done)" >/dev/null; t1=$(ms)
    echo "x$factor: copy into DuckDB file:              $((t1 - t0)) ms ($(du -h "$duck" | cut -f1) vs SQLite $(du -h "$an" | cut -f1))"
    t0=$(ms); duckdb -no-agent -init /dev/null -csv "$duck" < "$bench" > "$OUT/duck-native-x$factor.out"; t1=$(ms)
    echo "x$factor: bench DuckDB native:                $((t1 - t0)) ms"
    # same results in all three
    norm() { tr -d '"' < "$1" | grep -E '^[0-9]' | tr '|' ','; }
    if [ "$(norm "$OUT/sqlite-x$factor.out")" = "$(norm "$OUT/duck-scan-x$factor.out")" ] && [ "$(norm "$OUT/sqlite-x$factor.out")" = "$(norm "$OUT/duck-native-x$factor.out")" ]; then
      echo "x$factor: results identical"
    else
      echo "x$factor: RESULTS DIFFER"; paste "$OUT/sqlite-x$factor.out" "$OUT/duck-native-x$factor.out"
    fi
  done
done
