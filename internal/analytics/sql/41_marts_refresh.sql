-- Refresh of all marts, rebuilt whole, in the primary time zone (:tz). Local day, hour and week
-- come from bridge_time_slot; facts hold UTC only.
DROP VIEW IF EXISTS temp.ev_local;
CREATE TEMP VIEW ev_local AS
SELECT f.*, b.tz, b.local_date, b.local_hour, b.iso_weekday,
       a.actor, et.kind, h.host_name, p.project_name, t.tool_name, t.tool_kind, t.mcp_server,
       m.model, m.model_family
FROM fact_event f
JOIN bridge_time_slot b ON b.slot_key = f.slot_key
LEFT JOIN dim_host h ON h.host_key = f.host_key
LEFT JOIN dim_actor a ON a.actor_key = f.actor_key
LEFT JOIN dim_event_type et ON et.event_type_key = f.event_type_key
LEFT JOIN dim_project p ON p.project_key = f.project_key
LEFT JOIN dim_tool t ON t.tool_key = f.tool_key
LEFT JOIN dim_model m ON m.model_key = f.model_key;

DELETE FROM mart_daily;
INSERT INTO mart_daily
SELECT :tz, local_date, COALESCE(host_name, ''), COALESCE(project_name, ''),
       COUNT(DISTINCT CASE WHEN actor IN ('human', 'agent') THEN session_key END),
       SUM(actor = 'human'), SUM(actor = 'agent'), SUM(kind = 'tool_use'),
       SUM(tokens_in), SUM(tokens_out)
FROM ev_local WHERE tz = :tz
GROUP BY local_date, COALESCE(host_name, ''), COALESCE(project_name, '');

DELETE FROM mart_project_week;
INSERT INTO mart_project_week
SELECT :tz, d.iso_year, d.iso_week, COALESCE(e.project_name, ''),
       COUNT(DISTINCT CASE WHEN e.actor IN ('human', 'agent') THEN e.session_key END),
       COUNT(DISTINCT CASE WHEN e.actor IN ('human', 'agent') THEN e.local_date END),
       SUM(e.actor = 'human'), SUM(e.actor = 'agent'), SUM(e.kind = 'tool_use'),
       SUM(e.tokens_in), SUM(e.tokens_out)
FROM ev_local e
LEFT JOIN dim_date d ON d.date = e.local_date
WHERE e.tz = :tz
GROUP BY d.iso_year, d.iso_week, COALESCE(e.project_name, '');

DELETE FROM mart_tool_month;
INSERT INTO mart_tool_month
SELECT :tz, substr(local_date, 1, 7), COALESCE(project_name, ''), tool_name, tool_kind, mcp_server, COUNT(*)
FROM ev_local
WHERE tz = :tz AND kind = 'tool_use'
GROUP BY substr(local_date, 1, 7), COALESCE(project_name, ''), tool_name, tool_kind, mcp_server;

DELETE FROM mart_session_month;
INSERT INTO mart_session_month
SELECT :tz, substr(b.local_date, 1, 7), COALESCE(p.project_name, ''),
       COUNT(*), SUM(s.is_subagent),
       ROUND(AVG(s.duration_s) / 60.0, 1), ROUND(SUM(s.duration_s) / 3600.0, 1),
       SUM(s.human_events), SUM(s.agent_events),
       ROUND(1.0 * SUM(s.agent_events) / NULLIF(SUM(s.human_events), 0), 2),
       SUM(s.model_switches > 0)
FROM fact_session s
JOIN bridge_time_slot b ON b.slot_key = s.start_slot_key AND b.tz = :tz
LEFT JOIN dim_project p ON p.project_key = s.project_key
GROUP BY substr(b.local_date, 1, 7), COALESCE(p.project_name, '');

DELETE FROM mart_tokens_day;
INSERT INTO mart_tokens_day
SELECT :tz, local_date, COALESCE(model, ''), COALESCE(model_family, 'unknown'),
       COUNT(*), SUM(tokens_in), SUM(tokens_fresh), SUM(tokens_cache_read),
       SUM(tokens_cache_write), SUM(tokens_unsplit), SUM(tokens_out)
FROM ev_local
WHERE tz = :tz AND has_usage
GROUP BY local_date, COALESCE(model, ''), COALESCE(model_family, 'unknown');

DELETE FROM mart_weekhour;
INSERT INTO mart_weekhour
SELECT :tz, iso_weekday, local_hour, SUM(actor = 'human'), SUM(actor = 'agent')
FROM ev_local
WHERE tz = :tz AND actor IN ('human', 'agent')
GROUP BY iso_weekday, local_hour;
