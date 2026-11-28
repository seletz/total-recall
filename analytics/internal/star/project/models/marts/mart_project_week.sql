{{ config(materialized='table') }}
SELECT '{{ var('tz') }}' AS tz, d.iso_year, d.iso_week, COALESCE(e.project_name, '') AS project_name,
       count(DISTINCT CASE WHEN e.actor IN ('human', 'agent') THEN e.session_key END) AS sessions,
       count(DISTINCT CASE WHEN e.actor IN ('human', 'agent') THEN e.local_date END) AS active_days,
       count(*) FILTER (WHERE e.actor = 'human') AS human_events,
       count(*) FILTER (WHERE e.actor = 'agent') AS agent_events,
       count(*) FILTER (WHERE e.kind = 'tool_use') AS tool_calls,
       CAST(sum(e.tokens_in) AS BIGINT) AS tokens_in,
       CAST(sum(e.tokens_out) AS BIGINT) AS tokens_out
FROM {{ ref('int_event_local') }} e
LEFT JOIN {{ ref('dim_date') }} d ON d.date = e.local_date
GROUP BY d.iso_year, d.iso_week, COALESCE(e.project_name, '')
