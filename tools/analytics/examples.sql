-- Spike #349: example questions against the analytics_*_v1 views (SQLite;
-- in PostgreSQL the same with analytics.<name>_v1). Run with
--   sqlite3 build/analytics.db < tools/analytics/examples.sql
.mode markdown
.headers on
.print '### Q1 Projekte je Monat: Sessions, Subagenten, Dauer, Agent-Events je eigenem Prompt'
SELECT month, project, sessions, subagent_sessions AS subagents, avg_duration_min AS avg_min,
       total_duration_h AS hours, agent_per_human
FROM analytics_session_month_v1
WHERE month IN ('2026-09', '2026-10') ORDER BY month, sessions DESC;
.print '### Q2 Werkzeug-Mix je Projekt (Anteil der Tool-Aufrufe, Top 3)'
SELECT project, tool_name, calls, pct FROM (
  SELECT project, tool_name, SUM(calls) AS calls,
         ROUND(100.0 * SUM(calls) / SUM(SUM(calls)) OVER (PARTITION BY project), 1) AS pct,
         ROW_NUMBER() OVER (PARTITION BY project ORDER BY SUM(calls) DESC) AS rk
  FROM analytics_tool_month_v1 GROUP BY project, tool_name)
WHERE rk <= 3 ORDER BY project, calls DESC;
.print '### Q3 Wann arbeitet der Agent: Kernzeit (Mo-Fr 9-17 Uhr, Europe/Berlin) und außerhalb'
SELECT CASE WHEN iso_weekday <= 5 AND local_hour BETWEEN 9 AND 16 THEN 'Kernzeit'
            WHEN iso_weekday <= 5 THEN 'werktags außerhalb' ELSE 'Wochenende' END AS slot,
       SUM(human_events) AS human, SUM(agent_events) AS agent,
       ROUND(1.0 * SUM(agent_events) / NULLIF(SUM(human_events), 0), 2) AS agent_per_human
FROM analytics_weekhour_v1 GROUP BY 1 ORDER BY 1;
.print '### Q4 Tokens je Modellfamilie (Oktober), Cache-Anteil am Input'
SELECT model_family, SUM(events) AS events, SUM(tokens_in) AS tokens_in, SUM(tokens_out) AS tokens_out,
       ROUND(100.0 * SUM(tokens_cache_read) / SUM(tokens_in), 1) AS cache_read_pct,
       ROUND(100.0 * SUM(tokens_unsplit) / SUM(tokens_in), 1) AS unsplit_pct
FROM analytics_tokens_day_v1 WHERE day_local BETWEEN '2026-10-01' AND '2026-10-31'
GROUP BY 1 ORDER BY 1;
.print '### Q5 Projektwochen im Oktober: aktive Tage, Tool-Aufrufe je eigenem Prompt'
SELECT iso_week, project, active_days, sessions, human_events AS prompts, tool_calls,
       ROUND(1.0 * tool_calls / NULLIF(human_events, 0), 1) AS tools_per_prompt
FROM analytics_project_week_v1 WHERE iso_year = 2026 AND iso_week BETWEEN 41 AND 43
ORDER BY iso_week, project;
