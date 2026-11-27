-- Mart layer: one table per question a human asks, in the primary time
-- zone (etl_setting 'tz'; the column tz names it). Marts
-- are rebuilt from the facts on every load (delete the zone, insert): they
-- are cheap and never the only copy of anything. Parameter: :tz.

-- Grain as analytics.daily_v1 of #347: local day x host x project.
CREATE TABLE IF NOT EXISTS mart_daily (
	tz            TEXT NOT NULL,
	local_date    TEXT NOT NULL,
	host          TEXT NOT NULL,
	project       TEXT NOT NULL,
	sessions      INTEGER NOT NULL, -- distinct, with a human or agent event that day
	human_prompts INTEGER NOT NULL,
	agent_events  INTEGER NOT NULL,
	tool_calls    INTEGER NOT NULL,
	tokens_in     INTEGER NOT NULL,
	tokens_out    INTEGER NOT NULL,
	PRIMARY KEY (tz, local_date, host, project)
);

CREATE TABLE IF NOT EXISTS mart_project_week (
	tz           TEXT NOT NULL,
	iso_year     INTEGER NOT NULL,
	iso_week     INTEGER NOT NULL,
	project_name TEXT NOT NULL,
	sessions     INTEGER NOT NULL,
	active_days  INTEGER NOT NULL,
	human_events INTEGER NOT NULL,
	agent_events INTEGER NOT NULL,
	tool_calls   INTEGER NOT NULL,
	tokens_in    INTEGER NOT NULL,
	tokens_out   INTEGER NOT NULL,
	PRIMARY KEY (tz, iso_year, iso_week, project_name)
);

CREATE TABLE IF NOT EXISTS mart_tool_month (
	tz           TEXT NOT NULL,
	month        TEXT NOT NULL, -- YYYY-MM, local
	project_name TEXT NOT NULL,
	tool_name    TEXT NOT NULL,
	tool_kind    TEXT NOT NULL,
	mcp_server   TEXT NOT NULL,
	calls        INTEGER NOT NULL,
	PRIMARY KEY (tz, month, project_name, tool_name)
);

CREATE TABLE IF NOT EXISTS mart_session_month (
	tz                  TEXT NOT NULL,
	month               TEXT NOT NULL, -- local month of the session start
	project_name        TEXT NOT NULL,
	sessions            INTEGER NOT NULL,
	subagent_sessions   INTEGER NOT NULL,
	avg_duration_min    REAL NOT NULL,
	total_duration_h    REAL NOT NULL,
	human_events        INTEGER NOT NULL,
	agent_events        INTEGER NOT NULL,
	agent_per_human     REAL,            -- NULL without human events
	model_switch_sessions INTEGER NOT NULL,
	PRIMARY KEY (tz, month, project_name)
);

CREATE TABLE IF NOT EXISTS mart_tokens_day (
	tz                 TEXT NOT NULL,
	local_date         TEXT NOT NULL,
	model              TEXT NOT NULL,
	model_family       TEXT NOT NULL,
	events             INTEGER NOT NULL, -- events with usage
	tokens_in          INTEGER NOT NULL,
	tokens_fresh       INTEGER NOT NULL,
	tokens_cache_read  INTEGER NOT NULL,
	tokens_cache_write INTEGER NOT NULL,
	tokens_unsplit     INTEGER NOT NULL,
	tokens_out         INTEGER NOT NULL,
	PRIMARY KEY (tz, local_date, model)
);

CREATE TABLE IF NOT EXISTS mart_weekhour (
	tz           TEXT NOT NULL,
	iso_weekday  INTEGER NOT NULL,
	local_hour   INTEGER NOT NULL,
	human_events INTEGER NOT NULL,
	agent_events INTEGER NOT NULL,
	PRIMARY KEY (tz, iso_weekday, local_hour)
);
