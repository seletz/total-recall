-- Publication layer: the only objects consumers (notebooks, BI, the CLI,
-- the TUI) read. Names and versioning follow the analysis interface of
-- #347 (PR #352): analytics.<name>_v1 in PostgreSQL (schema analytics,
-- roles with SELECT only), analytics_<name>_v1 in SQLite, which has no
-- schemas. A view's columns only grow; a breaking change gets _v2 next to
-- _v1. The views decouple the interface from the physical star schema and
-- marts: a mart can become a view, a materialized view or move to another
-- engine without a consumer noticing. Their columns are a contract
-- (contract.txt), checked after every load.
--
-- The three views of #347 keep their names and columns (sessions_v1,
-- events_v1, daily_v1); differences in meaning are documented per column.
-- event_text_v1 of #347 does not exist here: no text, no full-text search
-- in the analytics store (FTS5 stays in the archive).
--
-- Local days: day_local is in the primary zone (etl_setting 'tz'); other
-- zones via analytics_time_slot_v1 and slot_key.

DROP VIEW IF EXISTS analytics_sessions_v1;
CREATE VIEW analytics_sessions_v1 AS
SELECT s.session_id, h.host_name AS host, hr.harness, hr.harness_version,
       p.project_name AS project,  -- #347: raw remote/root; here the project as stats --by project names it
       r.repo,                     -- the raw key: remote URL, git root or cwd (stats by_repo)
       s.git_branch, m.model,      -- model: first model of the agent (#347: sessions.model)
       ac.account_id, ac.account_name, ac.account_group,
       ps.session_id AS parent_id, s.is_subagent,
       f.start_utc AS started_at, f.end_utc AS ended_at,  -- first/last event, UTC
       f.duration_s, f.events, f.human_events, f.agent_events, f.tool_calls,
       f.distinct_models, f.model_switches, f.tokens_in, f.tokens_out
FROM fact_session f
LEFT JOIN dim_session s ON s.session_key = f.session_key
LEFT JOIN dim_session ps ON ps.session_key = s.parent_session_key
LEFT JOIN dim_host h ON h.host_key = f.host_key
LEFT JOIN dim_harness hr ON hr.harness_key = f.harness_key
LEFT JOIN dim_project p ON p.project_key = f.project_key
LEFT JOIN dim_repo r ON r.repo_key = f.repo_key
LEFT JOIN dim_model m ON m.model_key = f.first_model_key
LEFT JOIN dim_account ac ON ac.account_key = f.account_key;

DROP VIEW IF EXISTS analytics_events_v1;
CREATE VIEW analytics_events_v1 AS
SELECT f.event_id, s.session_id, h.host_name AS host, p.project_name AS project, r.repo,
       s.is_subagent, f.ts_utc AS ts, b.local_date AS day_local, b.local_hour AS hour_local,
       b.iso_weekday AS weekday_local, f.slot_key,
       et.kind, et.role, a.actor, et.input_kind, t.tool_name, t.tool_kind, t.mcp_server,
       m.model, m.model_family, hr.harness, ac.account_name, ac.account_group,
       f.tokens_in, f.tokens_out, f.tokens_cache_read, f.tokens_cache_write,
       f.tokens_fresh, f.tokens_unsplit
FROM fact_event f
LEFT JOIN bridge_time_slot b ON b.slot_key = f.slot_key
                            AND b.tz = (SELECT value FROM etl_setting WHERE key = 'tz')
LEFT JOIN dim_session s ON s.session_key = f.session_key
LEFT JOIN dim_host h ON h.host_key = f.host_key
LEFT JOIN dim_project p ON p.project_key = f.project_key
LEFT JOIN dim_repo r ON r.repo_key = f.repo_key
LEFT JOIN dim_harness hr ON hr.harness_key = f.harness_key
LEFT JOIN dim_account ac ON ac.account_key = f.account_key
LEFT JOIN dim_model m ON m.model_key = f.model_key
LEFT JOIN dim_tool t ON t.tool_key = f.tool_key
LEFT JOIN dim_actor a ON a.actor_key = f.actor_key
LEFT JOIN dim_event_type et ON et.event_type_key = f.event_type_key;

-- #347: sessions counts every session with any event; here only those with
-- a human or agent event, as stats does.
DROP VIEW IF EXISTS analytics_daily_v1;
CREATE VIEW analytics_daily_v1 AS
SELECT local_date AS day_local, host, project, sessions, human_prompts, agent_events,
       tool_calls, tokens_in, tokens_out
FROM mart_daily;

DROP VIEW IF EXISTS analytics_project_week_v1;
CREATE VIEW analytics_project_week_v1 AS
SELECT iso_year, iso_week, project_name AS project, sessions, active_days, human_events,
       agent_events, tool_calls, tokens_in, tokens_out
FROM mart_project_week;

DROP VIEW IF EXISTS analytics_tool_month_v1;
CREATE VIEW analytics_tool_month_v1 AS
SELECT month, project_name AS project, tool_name, tool_kind, mcp_server, calls
FROM mart_tool_month;

DROP VIEW IF EXISTS analytics_session_month_v1;
CREATE VIEW analytics_session_month_v1 AS
SELECT month, project_name AS project, sessions, subagent_sessions, avg_duration_min,
       total_duration_h, human_events, agent_events, agent_per_human, model_switch_sessions
FROM mart_session_month;

DROP VIEW IF EXISTS analytics_tokens_day_v1;
CREATE VIEW analytics_tokens_day_v1 AS
SELECT local_date AS day_local, model, model_family, events, tokens_in, tokens_fresh,
       tokens_cache_read, tokens_cache_write, tokens_unsplit, tokens_out
FROM mart_tokens_day;

DROP VIEW IF EXISTS analytics_weekhour_v1;
CREATE VIEW analytics_weekhour_v1 AS
SELECT iso_weekday, local_hour, human_events, agent_events
FROM mart_weekhour;

DROP VIEW IF EXISTS analytics_time_slot_v1;
CREATE VIEW analytics_time_slot_v1 AS
SELECT tz, slot_key, local_date, local_hour, iso_weekday FROM bridge_time_slot;
