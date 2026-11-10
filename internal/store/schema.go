package store

import (
	"context"
	"database/sql"
)

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
	// 4: continuation chain and account per session. continued_in holds the
	// full ID of the session this one was continued in (Claude: continued-in
	// record); no foreign key, the target may be ingested later or never.
	// account_id and organization_id come from the Claude session-owner
	// record. Sessions ingested before this version get the values on a
	// re-read of their transcripts (ingest --reread).
	`
ALTER TABLE sessions ADD COLUMN continued_in TEXT;
ALTER TABLE sessions ADD COLUMN account_id TEXT;
ALTER TABLE sessions ADD COLUMN organization_id TEXT;
CREATE INDEX sessions_continued_in ON sessions(continued_in);
CREATE INDEX sessions_account_id ON sessions(account_id);
`,
	// 5: raw as a trimmed, zstd-compressed BLOB and the source as a foreign
	// key (#132). sources gets an integer primary key (path stays unique);
	// events is rebuilt as events_v5 with source_id instead of source_path
	// and raw as BLOB. The SQL here only prepares the tables; the rows are
	// copied by migrationSteps[5] (migrateRawV5), which trims and compresses
	// raw, keeps seq (the rowid of the external-content FTS index, so the
	// index stays valid), swaps the tables and recreates indexes and
	// triggers (eventsIndexSQL). Events whose source_path has no sources row
	// get one with offset 0: the path is not read by anyone, the row only
	// keeps the reference.
	`
CREATE TABLE sources_v5 (
	id          INTEGER PRIMARY KEY,
	path        TEXT NOT NULL UNIQUE,
	harness     TEXT NOT NULL,
	size        INTEGER NOT NULL DEFAULT 0,
	mtime       TEXT,
	"offset"    INTEGER NOT NULL DEFAULT 0,
	ingested_at TEXT
);
INSERT INTO sources_v5 (path, harness, size, mtime, "offset", ingested_at)
	SELECT path, harness, size, mtime, "offset", ingested_at FROM sources ORDER BY rowid;
INSERT OR IGNORE INTO sources_v5 (path, harness)
	SELECT e.source_path,
	       COALESCE(MIN(s.harness), substr(e.session_id, 1, instr(e.session_id, ':') - 1))
	FROM events e LEFT JOIN sessions s ON s.id = e.session_id
	WHERE e.source_path IS NOT NULL AND e.source_path <> ''
	  AND e.source_path NOT IN (SELECT path FROM sources_v5)
	GROUP BY e.source_path;
DROP TABLE sources;
ALTER TABLE sources_v5 RENAME TO sources;

CREATE TABLE events_v5 (
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
	source_id     INTEGER REFERENCES sources(id),
	source_offset INTEGER,
	raw           BLOB,
	raw_truncated INTEGER NOT NULL DEFAULT 0
);
`,
	// 6: attachments (#181). blobs holds every image or document that was
	// delivered as base64 in a transcript line once per SHA-256, as
	// delivered (encoding '') or zstd-compressed ('zstd', see blob.go);
	// event_blobs links an event to its attachments in the order of the
	// references in its raw line (blobref). No foreign key to events: a
	// later rebuild of the events table (like migration 5) must stay
	// possible, and nothing deletes events. The migration moves no data:
	// `ingest --all --reread --rewrite-raw` fills raw and the blobs from the
	// transcripts afterwards (#180).
	`
CREATE TABLE blobs (
	sha256     TEXT PRIMARY KEY,
	media_type TEXT NOT NULL,
	bytes      INTEGER NOT NULL,
	encoding   TEXT NOT NULL DEFAULT '',
	data       BLOB NOT NULL
);
CREATE TABLE event_blobs (
	event_id TEXT NOT NULL,
	position INTEGER NOT NULL,
	sha256   TEXT NOT NULL REFERENCES blobs(sha256),
	PRIMARY KEY (event_id, position)
) WITHOUT ROWID;
CREATE INDEX event_blobs_sha256 ON event_blobs(sha256);
	`,
	// 7: meta records (#182): harness records that are no message but
	// belong to a session (Claude: attachments, queued commands, modes,
	// system prompts, file-history snapshots, workflow journal, types the
	// adapter does not know). One row per record with the whole line as
	// a redacted zstd BLOB like events.raw; no event, no full-text index,
	// no view reads it. id is the harness's record ID or a hash of
	// session, type and content, so the same content is stored once per
	// session and a reread is idempotent. ts may be NULL (records
	// without a time stamp). meta_blobs links a record to the large
	// values the adapter took out of its line (blobref references, like
	// event_blobs): a system prompt or the tool definitions are stored
	// once per hash across all sessions. The archive gets the records of
	// existing transcripts with ingest --all --reread.
	`
CREATE TABLE session_meta (
	seq           INTEGER PRIMARY KEY,
	id            TEXT NOT NULL UNIQUE,
	session_id    TEXT NOT NULL REFERENCES sessions(id),
	ts            TEXT,
	type          TEXT NOT NULL,
	source_id     INTEGER REFERENCES sources(id),
	source_offset INTEGER,
	raw           BLOB NOT NULL
);
CREATE INDEX session_meta_session ON session_meta(session_id, type);
CREATE TABLE meta_blobs (
	meta_id  TEXT NOT NULL,
	position INTEGER NOT NULL,
	sha256   TEXT NOT NULL REFERENCES blobs(sha256),
	PRIMARY KEY (meta_id, position)
) WITHOUT ROWID;
CREATE INDEX meta_blobs_sha256 ON meta_blobs(sha256);
`,
	// 8: project memory (#192): the memory files a harness keeps per
	// project (Claude Code: <projects_dir>/<project>/memory/*.md),
	// versioned. memory_projects holds one row per harness and native
	// project key (the adapter's, e.g. the directory name) with the
	// project's working directory once it is known (join dirs for the git
	// root). memory_files is the current state of every file ever seen,
	// relative to the memory directory: the hash of the current content
	// (NULL = deleted) and size and mtime on disk, so a scan reads only
	// files that changed. memory_versions has a row per content change
	// and per deletion (sha256 NULL); ts is the file's mtime for content,
	// the time of the scan for a deletion. The content is redacted text
	// in blobs (text/markdown, zstd), once per hash across the archive.
	// memory_fts indexes the current content of every existing file
	// (rowid = memory_files.id), apart from events_fts: the event search
	// and every statistic stay as they are. The migration moves no data;
	// the next ingest --all takes up the memory as it is on disk.
	`
CREATE TABLE memory_projects (
	id      INTEGER PRIMARY KEY,
	harness TEXT NOT NULL,
	key     TEXT NOT NULL,
	cwd     TEXT,
	UNIQUE (harness, key)
);
CREATE TABLE memory_files (
	id         INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES memory_projects(id),
	path       TEXT NOT NULL,
	sha256     TEXT REFERENCES blobs(sha256),
	size       INTEGER NOT NULL DEFAULT 0,
	mtime      TEXT,
	UNIQUE (project_id, path)
);
CREATE TABLE memory_versions (
	seq         INTEGER PRIMARY KEY,
	file_id     INTEGER NOT NULL REFERENCES memory_files(id),
	ts          TEXT NOT NULL,
	observed_at TEXT NOT NULL,
	sha256      TEXT REFERENCES blobs(sha256),
	bytes       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX memory_versions_file ON memory_versions(file_id, seq);
CREATE INDEX memory_versions_sha256 ON memory_versions(sha256);
CREATE VIRTUAL TABLE memory_fts USING fts5(
	text,
	tokenize = 'unicode61 remove_diacritics 2'
);
`,
	// 9: tokens_in stays the sum; the cached parts of it get their own
	// columns. migrateTokensV9 fills them from raw.
	`
ALTER TABLE events ADD COLUMN tokens_cache_read INTEGER;
ALTER TABLE events ADD COLUMN tokens_cache_write INTEGER;
`,
	// 10: how a human input reached the harness (typed, pasted, bash,
	// remote). NULL = unknown or not a human input. migrateInputKindV10
	// fills it from text and raw.
	`
ALTER TABLE events ADD COLUMN input_kind TEXT;
`,
}

// migrationNotes describe the migrations for `total-recall migrate`, one
// entry per entry of migrations (index+1 = version; a test checks that both
// lists have the same length). Like migrations, the list is append-only.
var migrationNotes = []migrationNote{
	{Summary: "initial schema: sources, sessions, events with full-text index, views exchanges and daily_activity"},
	{Summary: "sessions: harness-generated title (ai_title) and the derived display_name"},
	{Summary: "events: raw_truncated marks events whose raw line was dropped for its size"},
	{Summary: "sessions: continuation chain (continued_in), account_id and organization_id with indexes"},
	{Summary: "events: raw zstd-compressed (BLOB), source_id instead of source_path; " +
		"rewrites every event, about a minute per 100,000 events, and needs free disk space of about twice the archive size next to it (copy and WAL)",
		// Measured on a 1.1 GiB archive: the WAL grows to 0.36 times the
		// file (the new events table; dropped pages only join the freelist).
		Space: 0.5},
	{Summary: "blobs and event_blobs for attachments (images, documents) referenced from raw; moves no data, " +
		"run `ingest --all --reread --rewrite-raw` afterwards to fill raw and the blobs from the transcripts"},
	{Summary: "session_meta and meta_blobs: harness records that are no message (attachments, queued commands, modes, system prompts, file-history snapshots, unknown types) as redacted, compressed lines; " +
		"moves no data, the same `ingest --all --reread --rewrite-raw` fills them from the transcripts"},
	{Summary: "memory_projects, memory_files, memory_versions and memory_fts: the project memory of the harness (Claude Code: memory/*.md per project), versioned and redacted; " +
		"moves no data, the next `ingest --all` takes up the memory files as they are on disk"},
	{Summary: "events: tokens_cache_read and tokens_cache_write, the cached parts of tokens_in; " +
		"fills them from raw (usage) for the events that have it, a few seconds per 100,000 events with usage; the rest stays NULL"},
	{Summary: "events: input_kind (typed, pasted, bash, remote) of human messages; " +
		"fills it from text and raw for the human messages of the claude harness, seconds even on a large archive; events without raw stay NULL (bash and pasted are told from the text)"},
}

// migrationNote is the description of one migration.
type migrationNote struct {
	// Summary says in one line what changes and, when it matters, how long
	// it takes.
	Summary string
	// Space is the extra free space the migration needs next to the
	// database while it runs, as a factor of the database size: the WAL
	// grows by the pages the migration changes until the checkpoint after
	// it. 0 for a migration that touches few pages (ALTER TABLE, indexes
	// on small tables); 1 for one that rewrites every row.
	Space float64
}

// migrationStep is the Go part of a migration that cannot be expressed in
// SQL alone, for example rewriting every row. It runs in the migration's
// transaction after its SQL block and before schema_version is updated, so
// a failure leaves the database at the previous version. report publishes
// the progress (done of total work units, e.g. rows) to the caller of
// Migrate; call it every few thousand units.
type migrationStep func(ctx context.Context, tx *sql.Tx, report func(done, total int64)) error

// migrationSteps holds the Go part of the migrations that have one, keyed
// by version; each entry builds the step for the store that migrates, so a
// step can use the store's options. Most migrations are SQL only and have
// no entry.
var migrationSteps = map[int]func(s *Store) migrationStep{
	5:  func(s *Store) migrationStep { return s.migrateRawV5 },
	9:  func(s *Store) migrationStep { return s.migrateTokensV9 },
	10: func(s *Store) migrationStep { return s.migrateInputKindV10 },
}

// eventsIndexSQL creates the indexes and the full-text triggers of events.
// Migration 1 created the same objects; a rebuild of the table (migration
// 5) recreates them with this.
const eventsIndexSQL = `
CREATE INDEX events_session_ts ON events(session_id, ts);
CREATE INDEX events_ts ON events(ts);
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
`
