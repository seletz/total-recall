{{ config(materialized='table') }}
SELECT '{{ var('tz') }}' AS tz, strftime(local_date, '%Y-%m') AS month, COALESCE(project_name, '') AS project_name,
       tool_name, tool_kind, mcp_server, count(*) AS calls
FROM {{ ref('int_event_local') }}
WHERE kind = 'tool_use'
GROUP BY strftime(local_date, '%Y-%m'), COALESCE(project_name, ''), tool_name, tool_kind, mcp_server
