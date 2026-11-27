-- Analytics schema (spike #349): a constellation of two facts that share
-- conformed dimensions, a mart layer on top and read-only publication views
-- (pub_*) as the only interface for consumers.
--
-- Conventions
--   * Surrogate keys are deterministic hashes: tr_key(dimension, natural key
--     parts...) = first 8 bytes of md5 as a signed 64-bit integer (key.go).
--     The same key comes out of every load, every host and every target DB;
--     no sequence, no lookup on insert.
--   * Key 0 is never produced by tr_key for real values; each dimension
--     has an explicit "unknown" member instead (natural key ''), so a fact
--     always has a key. Consumers still join LEFT.
--   * SCD type 1: dimension attributes are overwritten on every load. The
--     archive is the audit trail; everything here can be rebuilt with
--     --full.
--   * Dimensions are accumulating supersets: a load upserts the keys it
--     sees and never deletes keys that no longer occur.
--   * Times are UTC (ts_utc, ISO 8601). Local days and hours come from the
--     bridge bridge_time_slot at query time, per time zone.
--   * No free text: no message text, no titles, no tool arguments, no raw
--     lines. Paths only with the home directory as "~".

-- Settings of the target, e.g. the primary time zone ('tz') the marts and
-- the *_v1 views use for local days.
CREATE TABLE IF NOT EXISTS etl_setting (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS etl_batch (
	batch_id        INTEGER PRIMARY KEY,
	source          TEXT NOT NULL, -- name of the source archive (host or 'central')
	mode            TEXT NOT NULL CHECK (mode IN ('full', 'incremental')),
	started_at      TEXT NOT NULL,
	finished_at     TEXT,
	seq_from        INTEGER NOT NULL, -- exclusive: events.seq > seq_from
	seq_to          INTEGER NOT NULL, -- inclusive high-water mark of the batch
	events_loaded   INTEGER NOT NULL DEFAULT 0,
	sessions_loaded INTEGER NOT NULL DEFAULT 0
);

-- One high-water mark per source archive: seq is per archive. With the
-- central archive of #347 there is one source for the whole pool.
CREATE TABLE IF NOT EXISTS etl_watermark (
	source   TEXT PRIMARY KEY,
	last_seq INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS dim_host (
	host_key  INTEGER PRIMARY KEY,
	host_name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS dim_harness (
	harness_key     INTEGER PRIMARY KEY,
	harness         TEXT NOT NULL,
	harness_version TEXT NOT NULL,
	UNIQUE (harness, harness_version)
);

CREATE TABLE IF NOT EXISTS dim_model (
	model_key    INTEGER PRIMARY KEY,
	model        TEXT NOT NULL UNIQUE, -- '' = unknown (human, tool results)
	model_family TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dim_tool (
	tool_key   INTEGER PRIMARY KEY,
	tool_name  TEXT NOT NULL UNIQUE, -- '' = no tool (messages)
	tool_kind  TEXT NOT NULL CHECK (tool_kind IN ('none', 'builtin', 'mcp')),
	mcp_server TEXT NOT NULL         -- '' unless tool_kind = 'mcp'
);

CREATE TABLE IF NOT EXISTS dim_actor (
	actor_key INTEGER PRIMARY KEY,
	actor     TEXT NOT NULL UNIQUE,
	is_human  INTEGER NOT NULL
);

-- Junk dimension: the small, low-cardinality event attributes.
CREATE TABLE IF NOT EXISTS dim_event_type (
	event_type_key INTEGER PRIMARY KEY,
	kind           TEXT NOT NULL,
	role           TEXT NOT NULL,
	input_kind     TEXT NOT NULL,
	UNIQUE (kind, role, input_kind)
);

-- Login of a session (subagents take their parent's). Name and group are
-- configuration (SCD 1), the IDs are the natural key.
CREATE TABLE IF NOT EXISTS dim_account (
	account_key     INTEGER PRIMARY KEY,
	account_id      TEXT NOT NULL,
	organization_id TEXT NOT NULL,
	account_name    TEXT NOT NULL,
	account_group   TEXT NOT NULL,
	UNIQUE (account_id, organization_id)
);

-- Project as stats --by project sees it: the raw key is the git root of the
-- session (subagents: of the parent), else its working directory. The name
-- comes from the [[project]] definitions and is recomputed on every load
-- (SCD 1), so a new definition renames without touching a fact.
CREATE TABLE IF NOT EXISTS dim_project (
	project_key  INTEGER PRIMARY KEY,
	project_path TEXT NOT NULL UNIQUE, -- raw key with home as "~"
	is_dir       INTEGER NOT NULL,     -- 1: working directory, no repository
	remote_url   TEXT NOT NULL,
	project_name TEXT NOT NULL
);

-- Repository as stats by_repo sees it: remote URL, else git root, else cwd
-- of the session itself.
CREATE TABLE IF NOT EXISTS dim_repo (
	repo_key INTEGER PRIMARY KEY,
	repo     TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS dim_session (
	session_key        INTEGER PRIMARY KEY,
	session_id         TEXT NOT NULL UNIQUE,
	parent_session_key INTEGER, -- NULL for main sessions
	continued_in_key   INTEGER, -- NULL when not continued
	is_subagent        INTEGER NOT NULL,
	git_branch         TEXT NOT NULL
);

-- Calendar per local date; joined via bridge_time_slot.local_date.
CREATE TABLE IF NOT EXISTS dim_date (
	date        TEXT PRIMARY KEY, -- YYYY-MM-DD
	year        INTEGER NOT NULL,
	month       INTEGER NOT NULL,
	iso_year    INTEGER NOT NULL,
	iso_week    INTEGER NOT NULL,
	iso_weekday INTEGER NOT NULL, -- 1 = Monday
	is_weekend  INTEGER NOT NULL
);

-- Time-zone bridge: one row per time zone and UTC quarter hour
-- (slot_key = unix seconds / 900). Every real zone offset is a multiple of
-- 15 minutes, so local day and hour boundaries are exact. Facts store only
-- UTC; a query picks the zone with WHERE tz = '...'.
CREATE TABLE IF NOT EXISTS bridge_time_slot (
	tz          TEXT NOT NULL,
	slot_key    INTEGER NOT NULL,
	local_date  TEXT NOT NULL,
	local_hour  INTEGER NOT NULL,
	iso_weekday INTEGER NOT NULL,
	PRIMARY KEY (tz, slot_key)
) WITHOUT ROWID;

-- Fact 1, grain: one archive event (message, tool call, tool result),
-- natural key events.id. event_count is implicitly 1 (COUNT(*)).
CREATE TABLE IF NOT EXISTS fact_event (
	event_key          INTEGER PRIMARY KEY,
	event_id           TEXT NOT NULL UNIQUE, -- degenerate dimension
	source_seq         INTEGER NOT NULL,     -- events.seq in the source archive
	batch_id           INTEGER NOT NULL,
	host_key           INTEGER NOT NULL,
	session_key        INTEGER NOT NULL,
	project_key        INTEGER NOT NULL,
	repo_key           INTEGER NOT NULL,
	harness_key        INTEGER NOT NULL,
	account_key        INTEGER NOT NULL,
	model_key          INTEGER NOT NULL,
	tool_key           INTEGER NOT NULL,
	actor_key          INTEGER NOT NULL,
	event_type_key     INTEGER NOT NULL,
	ts_utc             TEXT NOT NULL,
	slot_key           INTEGER NOT NULL,
	has_usage          INTEGER NOT NULL, -- tokens_in or tokens_out present
	tokens_in          INTEGER NOT NULL, -- whole input (= fresh+read+write+unsplit)
	tokens_fresh       INTEGER NOT NULL,
	tokens_cache_read  INTEGER NOT NULL,
	tokens_cache_write INTEGER NOT NULL,
	tokens_unsplit     INTEGER NOT NULL, -- input without a known split
	tokens_out         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS fact_event_session ON fact_event(session_key);
CREATE INDEX IF NOT EXISTS fact_event_slot ON fact_event(slot_key);

-- Fact 2, grain: one session. Rebuilt from fact_event for every session a
-- batch touched, so both facts always agree.
CREATE TABLE IF NOT EXISTS fact_session (
	session_key      INTEGER PRIMARY KEY,
	host_key         INTEGER NOT NULL,
	project_key      INTEGER NOT NULL,
	repo_key         INTEGER NOT NULL,
	harness_key      INTEGER NOT NULL,
	account_key      INTEGER NOT NULL,
	first_model_key  INTEGER NOT NULL,
	start_utc        TEXT NOT NULL,
	end_utc          TEXT NOT NULL,
	start_slot_key   INTEGER NOT NULL,
	duration_s       INTEGER NOT NULL,
	events           INTEGER NOT NULL,
	human_events     INTEGER NOT NULL,
	agent_events     INTEGER NOT NULL,
	tool_calls       INTEGER NOT NULL,
	distinct_models  INTEGER NOT NULL,
	model_switches   INTEGER NOT NULL,
	tokens_in        INTEGER NOT NULL,
	tokens_out       INTEGER NOT NULL,
	is_subagent      INTEGER NOT NULL
);
