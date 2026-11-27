-- Spike #349, variant "pool of files": the SQLite archives of the hosts
-- lie side by side (build/pool/<host>.db) and DuckDB queries them together
-- through its sqlite extension, without a collector service and without
-- copying. The host is the file name. Run from the repository root:
--   duckdb -init /dev/null < tools/analytics/duckdb/pool.sql
INSTALL sqlite; LOAD sqlite;
SET TimeZone = 'Europe/Berlin';
ATTACH 'build/pool/host-a.db' AS host_a (TYPE sqlite, READ_ONLY);
ATTACH 'build/pool/host-b.db' AS host_b (TYPE sqlite, READ_ONLY);
ATTACH 'build/demo.db' AS reference (TYPE sqlite, READ_ONLY);

-- The pool: one view over all host files, host from the file.
CREATE TEMP VIEW pool_events AS
SELECT 'host-a' AS host, e.session_id, e.ts, e.actor, e.kind, e.tool_name FROM host_a.events e
UNION ALL
SELECT 'host-b', e.session_id, e.ts, e.actor, e.kind, e.tool_name FROM host_b.events e;

.print '-- per host'
SELECT host, COUNT(DISTINCT session_id) AS sessions, COUNT(*) AS events,
       count_if(actor = 'human') AS human, count_if(actor = 'agent') AS agent,
       count_if(kind = 'tool_use') AS tool_calls
FROM pool_events GROUP BY ALL
UNION ALL
SELECT 'pool', COUNT(DISTINCT session_id), COUNT(*), count_if(actor = 'human'),
       count_if(actor = 'agent'), count_if(kind = 'tool_use') FROM pool_events
UNION ALL
SELECT 'reference', COUNT(DISTINCT session_id), COUNT(*), count_if(actor = 'human'),
       count_if(actor = 'agent'), count_if(kind = 'tool_use') FROM reference.events
ORDER BY 1;

.print '-- sum equals parts per local day (rows that differ; empty = equal)'
WITH p AS (
	SELECT CAST(CAST(ts AS TIMESTAMPTZ) AS DATE) AS day, COUNT(DISTINCT session_id) AS sessions,
	       count_if(actor = 'human') AS human, count_if(actor = 'agent') AS agent
	FROM pool_events WHERE actor IN ('human', 'agent') GROUP BY ALL
), r AS (
	SELECT CAST(CAST(ts AS TIMESTAMPTZ) AS DATE) AS day, COUNT(DISTINCT session_id) AS sessions,
	       count_if(actor = 'human') AS human, count_if(actor = 'agent') AS agent
	FROM reference.events WHERE actor IN ('human', 'agent') GROUP BY ALL
)
SELECT 'only pool' AS side, * FROM (SELECT * FROM p EXCEPT SELECT * FROM r)
UNION ALL
SELECT 'only reference', * FROM (SELECT * FROM r EXCEPT SELECT * FROM p);
