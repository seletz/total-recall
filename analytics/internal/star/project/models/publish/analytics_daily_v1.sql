SELECT local_date AS day_local, host, project, sessions, human_prompts, agent_events,
       tool_calls, tokens_in, tokens_out
FROM {{ ref('mart_daily') }}
