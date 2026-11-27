{{ config(materialized='table', contract={'enforced': true}) }}
-- Grain: local day x host x project (analytics.daily_v1). Portable SQL:
-- runs in PostgreSQL through dbt and in SQLite through total-recall.
SELECT b.local_date AS day_local,
       COALESCE(h.host_name, '') AS host,
       COALESCE(p.project_name, '') AS project,
       COUNT(DISTINCT CASE WHEN a.actor IN ('human', 'agent') THEN f.session_key END) AS sessions,
       SUM(CASE WHEN a.actor = 'human' THEN 1 ELSE 0 END) AS human_prompts,
       SUM(CASE WHEN a.actor = 'agent' THEN 1 ELSE 0 END) AS agent_events,
       SUM(CASE WHEN et.kind = 'tool_use' THEN 1 ELSE 0 END) AS tool_calls,
       SUM(f.tokens_in) AS tokens_in,
       SUM(f.tokens_out) AS tokens_out
FROM {{ source('tr', 'fact_event') }} f
JOIN {{ source('tr', 'bridge_time_slot') }} b ON b.slot_key = f.slot_key AND b.tz = '{{ var("tz") }}'
LEFT JOIN {{ source('tr', 'dim_host') }} h ON h.host_key = f.host_key
LEFT JOIN {{ source('tr', 'dim_project') }} p ON p.project_key = f.project_key
LEFT JOIN {{ source('tr', 'dim_actor') }} a ON a.actor_key = f.actor_key
LEFT JOIN {{ source('tr', 'dim_event_type') }} et ON et.event_type_key = f.event_type_key
GROUP BY b.local_date, COALESCE(h.host_name, ''), COALESCE(p.project_name, '')
