{{ config(materialized='table') }}
-- Sessions by the local month of their start.
SELECT '{{ var('tz') }}' AS tz, strftime(b.local_date, '%Y-%m') AS month, COALESCE(p.project_name, '') AS project_name,
       count(*) AS sessions,
       count(*) FILTER (WHERE s.is_subagent) AS subagent_sessions,
       round(avg(s.duration_s) / 60.0, 1) AS avg_duration_min,
       round(sum(s.duration_s) / 3600.0, 1) AS total_duration_h,
       CAST(sum(s.human_events) AS BIGINT) AS human_events,
       CAST(sum(s.agent_events) AS BIGINT) AS agent_events,
       round(1.0 * sum(s.agent_events) / NULLIF(sum(s.human_events), 0), 2) AS agent_per_human,
       count(*) FILTER (WHERE s.model_switches > 0) AS model_switch_sessions
FROM {{ ref('fact_session') }} s
JOIN {{ ref('bridge_time_slot') }} b ON b.slot_key = s.start_slot_key AND b.tz = '{{ var('tz') }}'
LEFT JOIN {{ ref('dim_project') }} p ON p.project_key = s.project_key
GROUP BY strftime(b.local_date, '%Y-%m'), COALESCE(p.project_name, '')
