SELECT month, project_name AS project, sessions, subagent_sessions, avg_duration_min,
       total_duration_h, human_events, agent_events, agent_per_human, model_switch_sessions
FROM {{ ref('mart_session_month') }}
