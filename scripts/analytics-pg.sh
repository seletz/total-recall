#!/usr/bin/env bash
# Spike #349: the star schema as the PostgreSQL projection of #347, in a
# throwaway container on 127.0.0.1:55449 (never a real database). Loads
# build/analytics.db into schema tr (DuckDB), creates analytics.*_v1 and
# the read-only role tr_analyst, builds the dbt model with dbt-core and
# dbt-postgres (uvx) and compares it with analytics.daily_v1.
# Remove the container afterwards: docker rm -f tr349-pg
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f build/demo.db ] || go run ./tools/demo -out build/demo.db
go run ./tools/analytics -archive build/demo.db -out build/analytics.db
if ! docker ps --format '{{.Names}}' | grep -qx tr349-pg; then
  docker run -d --rm --name tr349-pg -e POSTGRES_PASSWORD=spike -p 127.0.0.1:55449:5432 postgres:17-alpine >/dev/null
fi
until docker exec tr349-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
duckdb -no-agent -init /dev/null < tools/analytics/duckdb/to_postgres.sql
docker exec -i tr349-pg psql -U postgres -v ON_ERROR_STOP=1 -q < tools/analytics/postgres/publish.sql 2>&1 | grep -v 'does not exist, skipping' || true
(cd tools/analytics/dbt && uvx --quiet --from dbt-core --with dbt-postgres dbt build --project-dir . --profiles-dir .)
docker exec tr349-pg psql -U postgres -tAc "SELECT 'rows differing between dbt model and analytics.daily_v1: ' || COUNT(*) FROM (
  SELECT day_local, host, project, sessions, human_prompts, agent_events, tool_calls FROM analytics_dbt.daily
  EXCEPT SELECT day_local, host, project, sessions, human_prompts, agent_events, tool_calls FROM analytics.daily_v1) x"
for q in "SELECT count(*) AS daily_v1_rows FROM analytics.daily_v1" "SELECT count(*) FROM tr.fact_event" "CREATE TABLE analytics.x (i int)"; do
  echo "tr_analyst> $q"
  docker exec -e PGPASSWORD=analyst tr349-pg psql -h 127.0.0.1 -U tr_analyst -d postgres -tAc "$q" 2>&1 | head -1 || true
done
