{{ config(materialized='table') }}
-- Local day x host x project in the primary zone. sessions: distinct, with
-- a human or agent event that day (as stats).
SELECT '{{ var('tz') }}' AS tz, local_date, COALESCE(host_name, '') AS host, COALESCE(project_name, '') AS project,
       count(DISTINCT CASE WHEN actor IN ('human', 'agent') THEN session_key END) AS sessions,
       count(*) FILTER (WHERE actor = 'human') AS human_prompts,
       count(*) FILTER (WHERE actor = 'agent') AS agent_events,
       count(*) FILTER (WHERE kind = 'tool_use') AS tool_calls,
       CAST(sum(tokens_in) AS BIGINT) AS tokens_in,
       CAST(sum(tokens_out) AS BIGINT) AS tokens_out
FROM {{ ref('int_event_local') }}
GROUP BY local_date, COALESCE(host_name, ''), COALESCE(project_name, '')
