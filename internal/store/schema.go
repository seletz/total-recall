package store

// migrations are applied in order, each in its own transaction; the index+1
// is the schema version recorded in schema_version. Forward-only: never edit
// a released migration, append a new one.
//
// Notes on deviations from docs/explanation/design.md:
//   - events has an explicit INTEGER PRIMARY KEY "seq" and "id" is UNIQUE
//     (not the PK). events_fts uses external content keyed by rowid, and
//     VACUUM (including VACUUM INTO for backups) may renumber implicit rowids
//     of tables without an INTEGER PRIMARY KEY, which would silently break
//     the full-text index. seq also gives a stable insertion order.
//   - events.ts_local holds the event time in the store's time zone
//     (default Europe/Berlin), computed in Go at insert time, because SQLite
//     has no time zone database. daily_activity uses it; stats for other
//     time zones are computed in Go (Store.Stats).
//   - FTS triggers cover insert, delete and update so the index stays
//     consistent if rows are ever removed or corrected.
var migrations = []string{
	`
CREATE TABLE sources (
	path        TEXT PRIMARY KEY,
	harness     TEXT NOT NULL,
	size        INTEGER NOT NULL DEFAULT 0,
	mtime       TEXT,
	"offset"    INTEGER NOT NULL DEFAULT 0,
	ingested_at TEXT
);

CREATE TABLE sessions (
	id              TEXT PRIMARY KEY,
	harness         TEXT NOT NULL,
	harness_version TEXT,
	native_id       TEXT,
	parent_id       TEXT,
	name            TEXT,
	agent_name      TEXT,
	cwd             TEXT,
	git_root        TEXT,
	git_remote_url  TEXT,
	git_branch      TEXT,
	model           TEXT,
	started_at      TEXT,
	ended_at        TEXT
);
CREATE INDEX sessions_started_at ON sessions(started_at);
CREATE INDEX sessions_native_id ON sessions(native_id);
CREATE INDEX sessions_parent_id ON sessions(parent_id);

CREATE TABLE events (
	seq           INTEGER PRIMARY KEY,
	id            TEXT NOT NULL UNIQUE,
	session_id    TEXT NOT NULL REFERENCES sessions(id),
	ts            TEXT NOT NULL,
	ts_local      TEXT,
	kind          TEXT NOT NULL,
	role          TEXT,
	actor         TEXT,
	text          TEXT,
	tool_name     TEXT,
	model         TEXT,
	tokens_in     INTEGER,
	tokens_out    INTEGER,
	source_path   TEXT,
	source_offset INTEGER,
	raw           TEXT
);
CREATE INDEX events_session_ts ON events(session_id, ts);
CREATE INDEX events_ts ON events(ts);

CREATE TABLE dirs (
	cwd            TEXT PRIMARY KEY,
	git_root       TEXT,
	git_remote_url TEXT,
	resolved_at    TEXT
);

CREATE VIRTUAL TABLE events_fts USING fts5(
	text,
	content = 'events',
	content_rowid = 'seq',
	tokenize = 'unicode61 remove_diacritics 2'
);
CREATE TRIGGER events_fts_ai AFTER INSERT ON events BEGIN
	INSERT INTO events_fts(rowid, text) VALUES (new.seq, new.text);
END;
CREATE TRIGGER events_fts_ad AFTER DELETE ON events BEGIN
	INSERT INTO events_fts(events_fts, rowid, text) VALUES ('delete', old.seq, old.text);
END;
CREATE TRIGGER events_fts_au AFTER UPDATE OF text ON events BEGIN
	INSERT INTO events_fts(events_fts, rowid, text) VALUES ('delete', old.seq, old.text);
	INSERT INTO events_fts(rowid, text) VALUES (new.seq, new.text);
END;

-- One row per human message: the request and the last agent reply
-- (role=assistant, kind=message) before the next human message of the session.
CREATE VIEW exchanges AS
WITH h AS (
	SELECT seq, id, session_id, ts, text,
	       LEAD(ts) OVER (PARTITION BY session_id ORDER BY ts, seq) AS next_ts
	FROM events
	WHERE actor = 'human'
)
SELECT h.session_id AS session_id,
       h.ts         AS ts,
       h.text       AS request,
       a.text       AS response,
       h.id         AS request_id,
       a.id         AS response_id,
       a.ts         AS response_ts
FROM h
LEFT JOIN events a ON a.seq = (
	SELECT r.seq FROM events r
	WHERE r.session_id = h.session_id
	  AND r.role = 'assistant' AND r.kind = 'message'
	  AND r.ts >= h.ts
	  AND (h.next_ts IS NULL OR r.ts < h.next_ts)
	ORDER BY r.ts DESC, r.seq DESC
	LIMIT 1
);

-- Per local day (events.ts_local) and session: repo and human/agent event counts.
-- repo = git remote URL, else git root, else cwd.
CREATE VIEW daily_activity AS
SELECT substr(e.ts_local, 1, 10) AS day,
       e.session_id               AS session_id,
       COALESCE(NULLIF(s.git_remote_url, ''), NULLIF(s.git_root, ''), s.cwd) AS repo,
       SUM(e.actor = 'human')     AS human_events,
       SUM(e.actor = 'agent')     AS agent_events
FROM events e
JOIN sessions s ON s.id = e.session_id
GROUP BY day, e.session_id;
`,
	// 2: harness-generated titles (Claude ai-title) and a display name that
	// prefers the explicit title, then the generated one, then the agent name.
	`
ALTER TABLE sessions ADD COLUMN ai_title TEXT;
ALTER TABLE sessions ADD COLUMN display_name TEXT GENERATED ALWAYS AS
	(COALESCE(NULLIF(name, ''), NULLIF(ai_title, ''), NULLIF(agent_name, ''))) VIRTUAL;
`,
	// 3: raw lines above ingest.raw_max_bytes are not stored (raw = NULL);
	// raw_truncated marks those events so "no raw" is distinguishable from
	// "raw dropped because of its size".
	`
ALTER TABLE events ADD COLUMN raw_truncated INTEGER NOT NULL DEFAULT 0;
`,
}
