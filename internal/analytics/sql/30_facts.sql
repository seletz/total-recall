-- Transform, facts. Parameter: :batch_id.
--
-- fact_event is keyed on the natural grain (the event ID): a reload of the
-- same event, from a restaged session or from a second host that carries
-- the same transcript, replaces the row (the last load wins). That makes
-- every load idempotent.
INSERT INTO fact_event (
	event_key, event_id, source_seq, batch_id, host_key, session_key, project_key, repo_key,
	harness_key, account_key, model_key, tool_key, actor_key, event_type_key, ts_utc, slot_key,
	has_usage, tokens_in, tokens_fresh, tokens_cache_read, tokens_cache_write, tokens_unsplit, tokens_out)
SELECT tr_key('event', event_id), event_id, seq, :batch_id, tr_key('host', host),
       tr_key('session', session_id),
       tr_key('project', project_path),
       tr_key('repo', repo),
       tr_key('harness', harness, harness_version),
       tr_key('account', account_id, organization_id),
       tr_key('model', model),
       tr_key('tool', tool_name),
       tr_key('actor', actor),
       tr_key('event_type', kind, role, input_kind),
       ts, slot_key,
       tokens_in IS NOT NULL OR tokens_out IS NOT NULL,
       COALESCE(tokens_in, 0),
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL
            THEN COALESCE(tokens_in, 0) - tokens_cache_read - tokens_cache_write ELSE 0 END,
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL THEN tokens_cache_read ELSE 0 END,
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL THEN tokens_cache_write ELSE 0 END,
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL THEN 0 ELSE COALESCE(tokens_in, 0) END,
       COALESCE(tokens_out, 0)
FROM stg_event WHERE true
ON CONFLICT (event_key) DO UPDATE SET
	source_seq = excluded.source_seq, batch_id = excluded.batch_id, host_key = excluded.host_key,
	session_key = excluded.session_key, project_key = excluded.project_key, repo_key = excluded.repo_key,
	harness_key = excluded.harness_key, account_key = excluded.account_key, model_key = excluded.model_key,
	tool_key = excluded.tool_key, actor_key = excluded.actor_key, event_type_key = excluded.event_type_key,
	ts_utc = excluded.ts_utc, slot_key = excluded.slot_key, has_usage = excluded.has_usage,
	tokens_in = excluded.tokens_in, tokens_fresh = excluded.tokens_fresh,
	tokens_cache_read = excluded.tokens_cache_read, tokens_cache_write = excluded.tokens_cache_write,
	tokens_unsplit = excluded.tokens_unsplit, tokens_out = excluded.tokens_out;

-- fact_session: rebuilt from fact_event for the touched sessions.
DROP TABLE IF EXISTS temp.stg_session;
CREATE TEMP TABLE stg_session AS
SELECT DISTINCT tr_key('session', session_id) AS session_key FROM stg_event;

DELETE FROM fact_session WHERE session_key IN (SELECT session_key FROM stg_session);

INSERT INTO fact_session
WITH ev AS (
	SELECT f.*, a.actor, m.model, et.kind,
	       ROW_NUMBER() OVER (PARTITION BY f.session_key ORDER BY f.ts_utc, f.source_seq) AS rn
	FROM fact_event f
	JOIN stg_session t ON t.session_key = f.session_key
	LEFT JOIN dim_actor a ON a.actor_key = f.actor_key
	LEFT JOIN dim_model m ON m.model_key = f.model_key
	LEFT JOIN dim_event_type et ON et.event_type_key = f.event_type_key
), modeled AS (
	SELECT session_key, model_key, rn,
	       LAG(model_key) OVER (PARTITION BY session_key ORDER BY rn) AS prev_model_key,
	       FIRST_VALUE(model_key) OVER (PARTITION BY session_key ORDER BY rn) AS first_model_key
	FROM ev WHERE actor = 'agent' AND model <> ''
), switches AS (
	SELECT session_key,
	       SUM(prev_model_key IS NOT NULL AND prev_model_key <> model_key) AS model_switches,
	       COUNT(DISTINCT model_key) AS distinct_models,
	       MAX(first_model_key) AS first_model_key
	FROM modeled GROUP BY session_key
)
SELECT ev.session_key,
       MAX(ev.host_key), MAX(ev.project_key), MAX(ev.repo_key), MAX(ev.harness_key), MAX(ev.account_key),
       COALESCE(MAX(sw.first_model_key), tr_key('model', '')),
       MIN(ev.ts_utc), MAX(ev.ts_utc),
       MIN(ev.slot_key),
       CAST(strftime('%s', MAX(ev.ts_utc)) AS INTEGER) - CAST(strftime('%s', MIN(ev.ts_utc)) AS INTEGER),
       COUNT(*),
       SUM(ev.actor = 'human'),
       SUM(ev.actor = 'agent'),
       SUM(ev.kind = 'tool_use'),
       COALESCE(MAX(sw.distinct_models), 0),
       COALESCE(MAX(sw.model_switches), 0),
       SUM(ev.tokens_in), SUM(ev.tokens_out),
       MAX(s.is_subagent)
FROM ev
LEFT JOIN switches sw ON sw.session_key = ev.session_key
LEFT JOIN dim_session s ON s.session_key = ev.session_key
GROUP BY ev.session_key;
