{{ config(materialized='incremental', unique_key='event_key') }}
-- Fact 1, grain: one archive event (message, tool call, tool result),
-- natural key events.id. Keyed on the natural grain: a reload of the same
-- event, from a restaged session or from a second host that carries the
-- same transcript, replaces the row (the last load wins), so every run is
-- idempotent. Joins to the dimensions are LEFT in every consumer; each
-- dimension has an unknown member (natural key '').
SELECT tr_key(concat_ws(chr(31), 'event', event_id)) AS event_key,
       event_id,
       source_name,
       seq AS source_seq,
       CAST({{ var('batch_id') }} AS BIGINT) AS batch_id,
       tr_key(concat_ws(chr(31), 'host', host)) AS host_key,
       tr_key(concat_ws(chr(31), 'session', session_id)) AS session_key,
       tr_key(concat_ws(chr(31), 'project', project_path)) AS project_key,
       tr_key(concat_ws(chr(31), 'repo', repo)) AS repo_key,
       tr_key(concat_ws(chr(31), 'harness', harness, harness_version)) AS harness_key,
       tr_key(concat_ws(chr(31), 'account', account_id, organization_id)) AS account_key,
       tr_key(concat_ws(chr(31), 'model', model)) AS model_key,
       tr_key(concat_ws(chr(31), 'tool', tool_name)) AS tool_key,
       tr_key(concat_ws(chr(31), 'actor', actor)) AS actor_key,
       tr_key(concat_ws(chr(31), 'event_type', kind, role, input_kind)) AS event_type_key,
       ts_utc,
       slot_key,
       tokens_in IS NOT NULL OR tokens_out IS NOT NULL AS has_usage,
       -- input = fresh + cache read + cache write + unsplit
       COALESCE(tokens_in, 0) AS tokens_in,
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL
            THEN COALESCE(tokens_in, 0) - tokens_cache_read - tokens_cache_write ELSE 0 END AS tokens_fresh,
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL THEN tokens_cache_read ELSE 0 END AS tokens_cache_read,
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL THEN tokens_cache_write ELSE 0 END AS tokens_cache_write,
       CASE WHEN tokens_cache_read IS NOT NULL AND tokens_cache_write IS NOT NULL THEN 0 ELSE COALESCE(tokens_in, 0) END AS tokens_unsplit,
       COALESCE(tokens_out, 0) AS tokens_out
FROM {{ ref('stg_event') }}
