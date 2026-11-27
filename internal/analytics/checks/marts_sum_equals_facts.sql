-- Sum equals parts inside the store: each mart sums to the facts.
SELECT 'daily' AS mart, m.tz, m.n, (SELECT COUNT(*) FROM fact_event f JOIN dim_actor a ON a.actor_key = f.actor_key WHERE a.actor IN ('human', 'agent')) AS facts
FROM (SELECT tz, SUM(human_prompts + agent_events) AS n FROM mart_daily GROUP BY tz) m
WHERE m.n <> (SELECT COUNT(*) FROM fact_event f JOIN dim_actor a ON a.actor_key = f.actor_key WHERE a.actor IN ('human', 'agent'))
UNION ALL
SELECT 'project_week', m.tz, m.n, (SELECT COUNT(*) FROM fact_event f JOIN dim_actor a ON a.actor_key = f.actor_key WHERE a.actor IN ('human', 'agent'))
FROM (SELECT tz, SUM(human_events + agent_events) AS n FROM mart_project_week GROUP BY tz) m
WHERE m.n <> (SELECT COUNT(*) FROM fact_event f JOIN dim_actor a ON a.actor_key = f.actor_key WHERE a.actor IN ('human', 'agent'))
UNION ALL
SELECT 'tool_month', m.tz, m.n, (SELECT COUNT(*) FROM fact_event f JOIN dim_event_type et ON et.event_type_key = f.event_type_key WHERE et.kind = 'tool_use')
FROM (SELECT tz, SUM(calls) AS n FROM mart_tool_month GROUP BY tz) m
WHERE m.n <> (SELECT COUNT(*) FROM fact_event f JOIN dim_event_type et ON et.event_type_key = f.event_type_key WHERE et.kind = 'tool_use')
UNION ALL
SELECT 'session_month', m.tz, m.n, (SELECT COUNT(*) FROM fact_session)
FROM (SELECT tz, SUM(sessions) AS n FROM mart_session_month GROUP BY tz) m
WHERE m.n <> (SELECT COUNT(*) FROM fact_session)
UNION ALL
SELECT 'tokens_day', m.tz, m.n, (SELECT SUM(tokens_in + tokens_out) FROM fact_event)
FROM (SELECT tz, SUM(tokens_in + tokens_out) AS n FROM mart_tokens_day GROUP BY tz) m
WHERE m.n <> (SELECT SUM(tokens_in + tokens_out) FROM fact_event)
