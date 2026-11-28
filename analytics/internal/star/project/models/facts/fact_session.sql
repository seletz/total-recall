{{ config(materialized='incremental', unique_key='session_key') }}
-- Fact 2, grain: one session. Recomputed from fact_event for every session
-- the run touched, so both facts always agree. Start and end are the first
-- and last event (UTC), present in every archive.
WITH touched AS (
	SELECT DISTINCT tr_key(concat_ws(chr(31), 'session', session_id)) AS session_key FROM {{ ref('stg_event') }}
), ev AS (
	SELECT f.*, a.actor, m.model, et.kind,
	       row_number() OVER (PARTITION BY f.session_key ORDER BY f.ts_utc, f.source_seq) AS rn
	FROM {{ ref('fact_event') }} f
	JOIN touched t ON t.session_key = f.session_key
	LEFT JOIN {{ ref('dim_actor') }} a ON a.actor_key = f.actor_key
	LEFT JOIN {{ ref('dim_model') }} m ON m.model_key = f.model_key
	LEFT JOIN {{ ref('dim_event_type') }} et ON et.event_type_key = f.event_type_key
), modeled AS (
	SELECT session_key, model_key, rn,
	       lag(model_key) OVER (PARTITION BY session_key ORDER BY rn) AS prev_model_key,
	       first_value(model_key) OVER (PARTITION BY session_key ORDER BY rn) AS first_model_key
	FROM ev WHERE actor = 'agent' AND model <> ''
), switches AS (
	SELECT session_key,
	       count(*) FILTER (WHERE prev_model_key IS NOT NULL AND prev_model_key <> model_key) AS model_switches,
	       count(DISTINCT model_key) AS distinct_models,
	       max(first_model_key) AS first_model_key
	FROM modeled GROUP BY session_key
)
SELECT ev.session_key,
       max(ev.host_key) AS host_key,
       max(ev.project_key) AS project_key,
       max(ev.repo_key) AS repo_key,
       max(ev.harness_key) AS harness_key,
       max(ev.account_key) AS account_key,
       COALESCE(max(sw.first_model_key), tr_key(concat_ws(chr(31), 'model', ''))) AS first_model_key,
       min(ev.ts_utc) AS start_utc,
       max(ev.ts_utc) AS end_utc,
       min(ev.slot_key) AS start_slot_key,
       CAST(epoch(max(ev.ts_utc)) - epoch(min(ev.ts_utc)) AS BIGINT) AS duration_s,
       count(*) AS events,
       count(*) FILTER (WHERE ev.actor = 'human') AS human_events,
       count(*) FILTER (WHERE ev.actor = 'agent') AS agent_events,
       count(*) FILTER (WHERE ev.kind = 'tool_use') AS tool_calls,
       COALESCE(max(sw.distinct_models), 0) AS distinct_models,
       COALESCE(max(sw.model_switches), 0) AS model_switches,
       CAST(sum(ev.tokens_in) AS BIGINT) AS tokens_in,
       CAST(sum(ev.tokens_out) AS BIGINT) AS tokens_out,
       COALESCE(bool_or(s.is_subagent), false) AS is_subagent
FROM ev
LEFT JOIN switches sw ON sw.session_key = ev.session_key
LEFT JOIN {{ ref('dim_session') }} s ON s.session_key = ev.session_key
GROUP BY ev.session_key
