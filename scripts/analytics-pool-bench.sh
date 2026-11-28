#!/usr/bin/env bash
# Measure total-recall-analytics on a pool directory (#362): the demo pool
# (tools/demo -pool, two hosts) scaled by the factors given (default
# 1 10 100). Each factor clones every session and event of a host archive
# (new IDs, seq shifted, no raw lines); the pool copy comes from the real
# `total-recall backup` with [backup] pool_dir. Then it times
# `build --pool` twice (full, then incremental without anything new) and
# three queries. Synthetic data only; everything goes to $OUT (default
# build/pool-bench). Needs sqlite3 and the DuckDB libraries
# (mise run analytics:duckdb).
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=${OUT:-build/pool-bench}
rm -rf "$OUT"
mkdir -p "$OUT"
go run ./tools/demo -pool "$OUT/src" 2>/dev/null
go build -o "$OUT/total-recall" ./cmd/total-recall
(cd analytics && CGO_ENABLED=1 GOFLAGS=-tags=duckdb_use_static_lib go build -o "../$OUT/total-recall-analytics" ./cmd/total-recall-analytics)
ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
for factor in "${@:-1 10 100}"; do
  dir="$OUT/x$factor"
  mkdir -p "$dir"
  events=0
  for h in host-a host-b; do
    arch="$dir/$h.db"
    sqlite3 "$OUT/src/$h.db" "VACUUM INTO '$arch'"
    if [ "$factor" -gt 1 ]; then
      ecols=$(sqlite3 "$arch" "SELECT group_concat(name) FROM pragma_table_info('events')")
      scols=$(sqlite3 "$arch" "SELECT group_concat(name) FROM pragma_table_info('sessions')")
      esel=$(printf '%s' "$ecols" | sed -e 's/^seq,id,session_id,/seq + k.n * m.s, e.id || '"'"'-x'"'"' || k.n, e.session_id || '"'"'-x'"'"' || k.n,/' -e 's/,raw,/,NULL,/')
      ssel=$(printf '%s' "$scols" | sed -e 's/^id,/s.id || '"'"'-x'"'"' || k.n,/' -e 's/,native_id,parent_id,/,native_id || '"'"'-x'"'"' || k.n, CASE WHEN parent_id IS NOT NULL THEN parent_id || '"'"'-x'"'"' || k.n END,/' -e 's/,continued_in,/, CASE WHEN continued_in IS NOT NULL THEN continued_in || '"'"'-x'"'"' || k.n END,/')
      sqlite3 "$arch" "
        WITH RECURSIVE k(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM k WHERE n < $factor - 1)
        INSERT INTO sessions ($scols) SELECT $ssel FROM sessions s, k;
        WITH RECURSIVE k(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM k WHERE n < $factor - 1),
             m(s) AS (SELECT max(seq) FROM events)
        INSERT INTO events ($ecols) SELECT $esel FROM events e, k, m;"
    fi
    events=$((events + $(sqlite3 "$arch" "SELECT count(*) FROM events")))
    cat > "$dir/$h.toml" <<TOML
db = "$PWD/$arch"
host = "$h"
[backup]
dir = "$PWD/$dir/backups-$h"
pool_dir = "$PWD/$dir/pool"
TOML
    "$OUT/total-recall" --config "$dir/$h.toml" backup >/dev/null
  done
  cfg="$dir/host-a.toml"
  an=(--config "$cfg" --db "$dir/analytics.duckdb")
  t0=$(ms); "$OUT/total-recall-analytics" "${an[@]}" build --pool --cache "$dir/cache" >/dev/null; t1=$(ms)
  "$OUT/total-recall-analytics" "${an[@]}" build --pool --cache "$dir/cache" >/dev/null; t2=$(ms)
  "$OUT/total-recall-analytics" "${an[@]}" usage --by host --period month >/dev/null; t3=$(ms)
  "$OUT/total-recall-analytics" "${an[@]}" tokens --by model >/dev/null; t4=$(ms)
  "$OUT/total-recall-analytics" "${an[@]}" worktime --by category >/dev/null; t5=$(ms)
  got=$("$OUT/total-recall-analytics" "${an[@]}" query --format csv "SELECT count(*) FROM analytics_events_v1" | tail -1)
  [ "$got" = "$events" ] || { echo "x$factor: analytics has $got events, the archives $events" >&2; exit 1; }
  pool=$(du -sk "$dir/pool" | cut -f1) duck=$(du -k "$dir/analytics.duckdb" | cut -f1)
  printf 'x%-4s %9d events  pool %6d KiB  analytics %7d KiB  build full %6d ms  incremental %5d ms  usage %4d ms  tokens %4d ms  worktime %4d ms\n' \
    "$factor" "$events" "$pool" "$duck" $((t1 - t0)) $((t2 - t1)) $((t3 - t2)) $((t4 - t3)) $((t5 - t4))
done
