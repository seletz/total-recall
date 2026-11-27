SELECT iso_year, iso_week, project_name AS project, sessions, active_days, human_events,
       agent_events, tool_calls, tokens_in, tokens_out
FROM {{ ref('mart_project_week') }}
