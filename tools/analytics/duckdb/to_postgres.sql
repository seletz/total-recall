-- Spike #349: copy the star schema of build/analytics.db into PostgreSQL
-- (schema tr), the physical layer under the analytics.*_v1 views. Only
-- against a throwaway container (scripts/analytics-pg.sh), never a real DB.
INSTALL sqlite; LOAD sqlite; INSTALL postgres; LOAD postgres;
ATTACH 'build/analytics.db' AS a (TYPE sqlite, READ_ONLY);
ATTACH 'host=127.0.0.1 port=55449 user=postgres password=spike dbname=postgres' AS pg (TYPE postgres);
CALL postgres_execute('pg', 'DROP SCHEMA IF EXISTS analytics CASCADE');
CALL postgres_execute('pg', 'DROP SCHEMA IF EXISTS tr CASCADE');
CALL postgres_execute('pg', 'CREATE SCHEMA tr');
CALL postgres_clear_cache();
CREATE TABLE pg.tr.etl_setting AS SELECT * FROM a.etl_setting;
CREATE TABLE pg.tr.dim_host AS SELECT * FROM a.dim_host;
CREATE TABLE pg.tr.dim_harness AS SELECT * FROM a.dim_harness;
CREATE TABLE pg.tr.dim_model AS SELECT * FROM a.dim_model;
CREATE TABLE pg.tr.dim_tool AS SELECT * FROM a.dim_tool;
CREATE TABLE pg.tr.dim_actor AS SELECT actor_key, actor, is_human::BOOLEAN AS is_human FROM a.dim_actor;
CREATE TABLE pg.tr.dim_event_type AS SELECT * FROM a.dim_event_type;
CREATE TABLE pg.tr.dim_account AS SELECT * FROM a.dim_account;
CREATE TABLE pg.tr.dim_project AS SELECT project_key, project_path, is_dir::BOOLEAN AS is_dir, remote_url, project_name FROM a.dim_project;
CREATE TABLE pg.tr.dim_repo AS SELECT * FROM a.dim_repo;
CREATE TABLE pg.tr.dim_session AS SELECT session_key, session_id, parent_session_key, continued_in_key, is_subagent::BOOLEAN AS is_subagent, git_branch FROM a.dim_session;
CREATE TABLE pg.tr.dim_date AS SELECT * FROM a.dim_date;
CREATE TABLE pg.tr.bridge_time_slot AS SELECT * FROM a.bridge_time_slot;
CREATE TABLE pg.tr.fact_event AS SELECT * EXCLUDE (ts_utc, has_usage), CAST(ts_utc AS TIMESTAMPTZ) AS ts_utc, has_usage::BOOLEAN AS has_usage FROM a.fact_event;
CREATE TABLE pg.tr.fact_session AS SELECT * EXCLUDE (start_utc, end_utc, is_subagent), CAST(start_utc AS TIMESTAMPTZ) AS start_utc, CAST(end_utc AS TIMESTAMPTZ) AS end_utc, is_subagent::BOOLEAN AS is_subagent FROM a.fact_session;
CREATE TABLE pg.tr.mart_daily AS SELECT * FROM a.mart_daily;
CREATE TABLE pg.tr.mart_project_week AS SELECT * FROM a.mart_project_week;
CREATE TABLE pg.tr.mart_tool_month AS SELECT * FROM a.mart_tool_month;
CREATE TABLE pg.tr.mart_session_month AS SELECT * FROM a.mart_session_month;
CREATE TABLE pg.tr.mart_tokens_day AS SELECT * FROM a.mart_tokens_day;
CREATE TABLE pg.tr.mart_weekhour AS SELECT * FROM a.mart_weekhour;
SELECT (SELECT COUNT(*) FROM pg.tr.fact_event) AS pg_fact_event, (SELECT COUNT(*) FROM a.fact_event) AS sqlite_fact_event;
