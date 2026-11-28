-- Sum equals parts inside the store: each mart adds up to the facts.
WITH f AS (
	SELECT count(*) FILTER (WHERE a.actor IN ('human', 'agent')) AS acted,
	       count(*) FILTER (WHERE et.kind = 'tool_use') AS tool_calls,
	       COALESCE(sum(f.tokens_in + f.tokens_out), 0) AS tokens
	FROM {{ ref('fact_event') }} f
	LEFT JOIN {{ ref('dim_actor') }} a ON a.actor_key = f.actor_key
	LEFT JOIN {{ ref('dim_event_type') }} et ON et.event_type_key = f.event_type_key
)
SELECT 'daily' AS mart FROM f WHERE (SELECT COALESCE(sum(human_prompts + agent_events), 0) FROM {{ ref('mart_daily') }}) <> f.acted
UNION ALL SELECT 'project_week' FROM f WHERE (SELECT COALESCE(sum(human_events + agent_events), 0) FROM {{ ref('mart_project_week') }}) <> f.acted
UNION ALL SELECT 'weekhour' FROM f WHERE (SELECT COALESCE(sum(human_events + agent_events), 0) FROM {{ ref('mart_weekhour') }}) <> f.acted
UNION ALL SELECT 'tool_month' FROM f WHERE (SELECT COALESCE(sum(calls), 0) FROM {{ ref('mart_tool_month') }}) <> f.tool_calls
UNION ALL SELECT 'tokens_day' FROM f WHERE (SELECT COALESCE(sum(tokens_in + tokens_out), 0) FROM {{ ref('mart_tokens_day') }}) <> f.tokens
UNION ALL SELECT 'session_month' WHERE (SELECT COALESCE(sum(sessions), 0) FROM {{ ref('mart_session_month') }}) <> (SELECT count(*) FROM {{ ref('fact_session') }})
