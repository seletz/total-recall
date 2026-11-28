{{ config(materialized='view') }}
-- Events with their local time in the primary zone (var tz) and the
-- attributes the marts group by. Intermediate, not published.
SELECT f.*, b.local_date, b.local_hour, b.iso_weekday,
       a.actor, et.kind, h.host_name, p.project_name, t.tool_name, t.tool_kind, t.mcp_server,
       m.model, m.model_family
FROM {{ ref('fact_event') }} f
JOIN {{ ref('bridge_time_slot') }} b ON b.slot_key = f.slot_key AND b.tz = '{{ var('tz') }}'
LEFT JOIN {{ ref('dim_host') }} h ON h.host_key = f.host_key
LEFT JOIN {{ ref('dim_actor') }} a ON a.actor_key = f.actor_key
LEFT JOIN {{ ref('dim_event_type') }} et ON et.event_type_key = f.event_type_key
LEFT JOIN {{ ref('dim_project') }} p ON p.project_key = f.project_key
LEFT JOIN {{ ref('dim_tool') }} t ON t.tool_key = f.tool_key
LEFT JOIN {{ ref('dim_model') }} m ON m.model_key = f.model_key
