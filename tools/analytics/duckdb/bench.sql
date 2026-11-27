-- Spike #349: the mart computations as portable SQL (same text for SQLite
-- and DuckDB), timed against the star schema by scripts/analytics-bench.sh.
-- B1 daily: day x host x project (analytics_daily_v1)
SELECT COUNT(*), SUM(s), SUM(h), SUM(a), SUM(t) FROM (
SELECT b.local_date, ho.host_name, p.project_name,
       COUNT(DISTINCT CASE WHEN ac.actor IN ('human', 'agent') THEN f.session_key END) AS s,
       SUM(CASE WHEN ac.actor = 'human' THEN 1 ELSE 0 END) AS h,
       SUM(CASE WHEN ac.actor = 'agent' THEN 1 ELSE 0 END) AS a,
       SUM(CASE WHEN et.kind = 'tool_use' THEN 1 ELSE 0 END) AS t
FROM fact_event f
JOIN bridge_time_slot b ON b.slot_key = f.slot_key AND b.tz = 'Europe/Berlin'
LEFT JOIN dim_host ho ON ho.host_key = f.host_key
LEFT JOIN dim_project p ON p.project_key = f.project_key
LEFT JOIN dim_actor ac ON ac.actor_key = f.actor_key
LEFT JOIN dim_event_type et ON et.event_type_key = f.event_type_key
GROUP BY b.local_date, ho.host_name, p.project_name) x;
-- B2 tools: month x project x tool (analytics_tool_month_v1)
SELECT COUNT(*), SUM(n) FROM (
SELECT substr(b.local_date, 1, 7) AS m, p.project_name, t.tool_name, COUNT(*) AS n
FROM fact_event f
JOIN bridge_time_slot b ON b.slot_key = f.slot_key AND b.tz = 'Europe/Berlin'
JOIN dim_event_type et ON et.event_type_key = f.event_type_key AND et.kind = 'tool_use'
LEFT JOIN dim_project p ON p.project_key = f.project_key
LEFT JOIN dim_tool t ON t.tool_key = f.tool_key
GROUP BY substr(b.local_date, 1, 7), p.project_name, t.tool_name) x;
-- B3 week x hour heatmap (analytics_weekhour_v1)
SELECT COUNT(*), SUM(h), SUM(a) FROM (
SELECT b.iso_weekday, b.local_hour,
       SUM(CASE WHEN ac.actor = 'human' THEN 1 ELSE 0 END) AS h,
       SUM(CASE WHEN ac.actor = 'agent' THEN 1 ELSE 0 END) AS a
FROM fact_event f
JOIN bridge_time_slot b ON b.slot_key = f.slot_key AND b.tz = 'Europe/Berlin'
JOIN dim_actor ac ON ac.actor_key = f.actor_key AND ac.actor IN ('human', 'agent')
GROUP BY b.iso_weekday, b.local_hour) x;
-- B4 ad hoc: distinct sessions per project over the whole range (needs the facts, not a mart)
SELECT COUNT(*), SUM(s) FROM (
SELECT p.project_name, COUNT(DISTINCT f.session_key) AS s
FROM fact_event f LEFT JOIN dim_project p ON p.project_key = f.project_key
GROUP BY p.project_name) x;
