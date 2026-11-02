---
name: total-recall
description: Query the local archive of past coding-agent sessions (Claude Code and others) with the total-recall CLI. Use when the user asks about earlier agent sessions or their work history - what they worked on (in a repo, on a day, in a month), how much time or how many sessions went into something, what was discussed, asked or decided in a previous session (also in another repository), or where a topic came up before.
---

# total-recall

`total-recall` is a local SQLite archive of every coding-agent session on this machine, across all repositories. It holds the **conversation**: what the human asked, what the agent answered, which tools ran, with time stamps. This is how an agent in session A learns what was decided in session B, even when B ran in a repository that A cannot see.

Start with `total-recall sql --json` and full-text search (FTS5): `search` returns hits, `sql` returns answers, because it can group, count and join. Always pass `--json` and parse stdout only.

## The four core queries

Replace `sbom` with the topic. FTS5 syntax: `word`, `"exact phrase"`, `prefix*`, `a AND b`, `a OR b`, `a NOT b`. The `MATCH` operand must be the table name `events_fts` (an alias does not work), and `events_fts.rowid` joins to `events.seq`.

### 1. Which sessions talk about X, and when

```sh
total-recall sql --json "
SELECT s.id, s.display_name,
       coalesce(s.git_remote_url, s.git_root, s.cwd) AS repo,
       min(e.ts) AS first_hit, max(e.ts) AS last_hit, count(*) AS hits
FROM events_fts
JOIN events e ON e.seq = events_fts.rowid
JOIN sessions s ON s.id = e.session_id
WHERE events_fts MATCH 'sbom'
GROUP BY s.id
ORDER BY first_hit DESC
LIMIT 20"
```

One row per session with repo, time span and hit count. This is the right first query: it tells you where to read and gives the timeline for free.

### 2. What the human said about X

```sh
total-recall sql --json "
SELECT e.session_id, e.ts, e.text
FROM events_fts
JOIN events e ON e.seq = events_fts.rowid
WHERE events_fts MATCH 'sbom' AND e.actor = 'human'
ORDER BY e.ts
LIMIT 50"
```

`actor = 'human'` separates the user's own statements from thousands of agent and tool rows. For the agent's conclusions use `e.actor = 'agent' AND e.kind = 'message'`.

### 3. The conversation in a time window

```sh
total-recall sql --json "
SELECT ts, actor, kind, text
FROM events
WHERE session_id = 'claude:11111111-1111-4111-8111-111111111111'
  AND ts >= strftime('%Y-%m-%dT%H:%M:%fZ', '2026-10-05 23:00', 'utc')
  AND ts <  strftime('%Y-%m-%dT%H:%M:%fZ', '2026-10-06 00:00', 'utc')
  AND actor IN ('human', 'agent') AND kind = 'message'
ORDER BY ts
LIMIT 200"
```

The window is written in local clock time and converted to the stored UTC layout (see *Time stamps*). Drop the `actor`/`kind` line to include tool calls and results. The same window as a command:

```sh
total-recall show claude:11111111-1111-4111-8111-111111111111 --json --since 2026-10-05T23:00 --until 2026-10-06T00:00 --limit 200
```

### 4. Timeline of a topic

```sh
total-recall sql --json "
SELECT e.ts, coalesce(s.git_remote_url, s.git_root, s.cwd) AS repo,
       s.id AS session_id, e.actor,
       snippet(events_fts, 0, '[', ']', '…', 16) AS excerpt
FROM events_fts
JOIN events e ON e.seq = events_fts.rowid
JOIN sessions s ON s.id = e.session_id
WHERE events_fts MATCH 'sbom'
  AND e.actor IN ('human', 'agent') AND e.kind = 'message'
ORDER BY e.ts
LIMIT 100"
```

Chronological across sessions and repositories; the matched words are in brackets. For one row per local day and repo, group by `substr(e.ts_local, 1, 10), repo`.

### Follow-up: request and response pairs

Once a session and a time are known, the `exchanges` view gives each human message with the agent's reply:

```sh
total-recall sql --json "
SELECT ts, request, response
FROM exchanges
WHERE session_id = 'claude:11111111-1111-4111-8111-111111111111'
ORDER BY ts
LIMIT 50"
```

## Output contract

- Every read command (`status`, `sessions`, `show`, `search`, `stats`, `sql`, `doctor`, `config show`) has `--format table|json|jsonl`; `sql` also has `csv`. `--json` is short for `--format json`. Always use one of the JSON formats; only stdout needs parsing.
- Success: exit 0, data on stdout. An empty result is `[]` for `sessions`, `search` and `sql`; `show` returns `"events": []`; `jsonl` prints no line.
- Error: exit 1 and exactly one JSON object on stdout: `{"error":{"kind":"sql","message":"no such column: nosuchcol"}}`. `kind` is stable: `usage` (flags, arguments, their combinations), `config` (configuration not loadable), `db` (archive cannot be opened), `sql` (SQLite rejected the query, also `search --raw`), `not_found` (session or event unknown), `ambiguous` (prefix matches more than one session), `internal` (everything else). The message is repeated on stderr. Stdout is never empty.
- Every documented key is always present. A string without a value is `null` (e.g. `parent_id` of a top-level session), never a missing key; numbers are never `null` (0), lists never `null` (`[]`).
- `--format table` (the default without `--json`) truncates cells only when stdout is a terminal; a truncated cell ends with `…` and stderr says how many were cut. `--no-truncate` forces full cells. `json`, `jsonl` and `csv` never truncate.
- `--format jsonl` prints one compact object per line without buffering; use it for large reads. `status` and `stats` are then one line; `show --format jsonl` writes only the events, one per line (the session header comes from `sessions`).
- Time stamps in JSON are UTC (`2026-10-05T21:30:00.000Z`). `--since`/`--until` take local time: `YYYY-MM-DD`, `YYYY-MM-DDTHH:MM` or RFC 3339; a date-only `--until` includes that whole day.
- Session ids look like `claude:f2b299a1-b3c1-4cf8-b1d7-8c4840eab467` (`<harness>:<native id>`). `show` and `search --session` accept the full id, the native id or a unique prefix.

## Cost notes

- Put `LIMIT` on every exploratory query. The archive can hold hundreds of thousands of events, and a single session can have tens of thousands.
- `events.text` of tool results is truncated to 4 KiB. `events.raw` is the full original JSON line (NULL above 16 KiB, then `raw_truncated = 1`); never select `raw` in a list query.
- `show SESSION` without limits prints the whole session. Check `events` and `human_events` in `sessions --json` first, then window with `--since`/`--until`, page with `--limit`/`--offset`, or start at the `event_id` of a search hit with `--from-event`. `--all` adds tool calls, tool results, automation and system events and multiplies the volume.
- `search` returns 20 hits by default (`--limit N` for more). Narrow with `--since`, `--until`, `--repo`, `--actor`, `--session`.
- When listing many rows, select `snippet(events_fts, 0, '[', ']', '…', 16)` or `substr(text, 1, 300)` instead of `text`.

## Schema and pitfalls

### Time stamps

All times in the database are UTC ISO 8601 text with `T` and `Z`: `2026-10-05T21:30:00.000Z` (`events.ts`, `sessions.started_at`, `sessions.ended_at`). Tables printed by the CLI show local time; take times for a window from JSON, not from a table.

**Pitfall:** `datetime('2026-10-05 23:00', 'utc')` yields `2026-10-05 21:00:00`, a layout that never matches `ts` in a string comparison (the space sorts before `T`, so the window is empty or wrong). Convert a local clock time to the stored layout with:

```sql
WHERE ts >= strftime('%Y-%m-%dT%H:%M:%fZ', '2026-10-05 23:00', 'utc')
  AND ts <  strftime('%Y-%m-%dT%H:%M:%fZ', '2026-10-06 00:00', 'utc')
```

The `'utc'` modifier interprets the literal in the machine's time zone. Date-only literals are safe without conversion: `ts >= '2026-10-05' AND ts < '2026-10-06'` is that UTC day.

`events.ts_local` is the same instant in the configured zone (`timezone`, default `Europe/Berlin`), e.g. `2026-10-05T23:30:00.000+02:00`. Local day: `substr(ts_local, 1, 10)`; local window inside one session: `ts_local >= '2026-10-05T23:00' AND ts_local < '2026-10-06T00:00'` (no index on `ts_local`). For display, `datetime(ts, 'localtime')` converts with the machine's zone.

### Actors

`events.actor`: `human` = typed by the user (also `!cmd` bash input and pasted content); `agent` = the assistant; `peer` = another agent session or a subagent hand-back; `automation` = scheduled prompts, system reminders, slash-command plumbing, continuation summaries; `tool` = tool results. "What did the user say or decide" is `actor = 'human'`; the agent's conclusions are `actor = 'agent' AND kind = 'message'`. Ignore `automation` when counting real work.

### Subagents

Subagents are separate sessions with `parent_id` set and a native id like `agent-a01`. `sessions` hides them unless `--subagents`. Exclude them with `parent_id IS NULL` when counting sessions, but keep them when searching: their events carry the subagent's session id, not the parent's, and the decisive conversation may be there.

### Computed fields of `sessions --json`

`repo`, `duration_seconds`, `human_events`, `agent_events` and `events` are computed by the command. They are **not** columns of the `sessions` table; `SELECT s.events FROM sessions s` fails with `no such column`. In SQL use `coalesce(git_remote_url, git_root, cwd)`, `julianday(ended_at) - julianday(started_at)` and counts over `events`. `display_name` is a generated column and works in SQL.

### Tables and views

Inspect the live schema if anything here does not match:

```sh
total-recall sql --json "SELECT name, sql FROM sqlite_master WHERE type IN ('table','view')"
```

#### `sessions`

| column | meaning |
|---|---|
| `id` | PK, `<harness>:<native id>` |
| `harness` | e.g. `claude` |
| `harness_version` | last seen harness version |
| `native_id` | session id of the harness |
| `parent_id` | parent session for subagents, else NULL |
| `name` | explicit title set by the user (Claude `/rename`), authoritative |
| `ai_title` | title generated by the harness |
| `agent_name` | name of a subagent |
| `display_name` | generated: `COALESCE(name, ai_title, agent_name)`; use this for display |
| `cwd` | first seen working directory |
| `git_root` | main repository root (worktrees resolved to the main repo) |
| `git_remote_url` | normalized, e.g. `https://github.com/owner/repo`, no credentials |
| `git_branch` | last seen branch |
| `model` | last seen model |
| `started_at` | first event time (UTC) |
| `ended_at` | last event time (UTC) |

#### `events`

| column | meaning |
|---|---|
| `seq` | INTEGER PK, insertion order; `events_fts.rowid` refers to it |
| `id` | unique event id, `<harness>:<native id>` (plus `#<block>` for multi-block messages) |
| `session_id` | FK to `sessions.id` |
| `ts` | time stamp (UTC) |
| `ts_local` | same instant in the configured time zone, with offset |
| `kind` | `message`, `tool_use`, `tool_result`, `system`, `meta` |
| `role` | `user`, `assistant`, `system`, `tool` |
| `actor` | `human`, `agent`, `peer`, `automation`, `tool` (see *Actors*) |
| `text` | readable, redacted text; tool results truncated to 4 KiB |
| `tool_name` | for `tool_use` |
| `model` | model of the assistant message |
| `tokens_in` | input tokens, NULL if unknown |
| `tokens_out` | output tokens, NULL if unknown |
| `source_path` | transcript file the line came from |
| `source_offset` | byte offset of the line in that file |
| `raw` | original JSON line, redacted; NULL above `ingest.raw_max_bytes` (default 16 KiB) or with `store_raw = false` |
| `raw_truncated` | 1 if `raw` was dropped because of its size (`text` is still filled) |

#### `events_fts`

FTS5 index over `events.text` (external content, `rowid` = `events.seq`).

| column | meaning |
|---|---|
| `text` | the indexed text; `snippet(events_fts, 0, …)` and `rank` work as usual |

#### `exchanges`

View: one row per human message with the last agent reply (`role = 'assistant'`, `kind = 'message'`) before the next human message of the same session. Seven columns:

| column | meaning |
|---|---|
| `session_id` | session of the pair |
| `ts` | time of the request (UTC) |
| `request` | text of the human message |
| `response` | text of the agent reply, NULL if none |
| `request_id` | `events.id` of the request |
| `response_id` | `events.id` of the reply, NULL if none |
| `response_ts` | time of the reply (UTC), NULL if none |

#### `daily_activity`

View: per local day (configured zone) and session.

| column | meaning |
|---|---|
| `day` | `YYYY-MM-DD` local |
| `session_id` | |
| `repo` | `git_remote_url`, else `git_root`, else `cwd` |
| `human_events` | count of `actor = 'human'` |
| `agent_events` | count of `actor = 'agent'` |

#### Housekeeping tables

`sources` (`path`, `harness`, `size`, `mtime`, `offset`, `ingested_at`): ingested transcript files. `dirs` (`cwd`, `git_root`, `git_remote_url`, `resolved_at`): cache of git lookups. `schema_version` (`version`): schema version of the database.

### Other caveats

- **Redaction:** secrets were masked before storage as `«redacted:<kind>»`. The originals are gone; still treat the content as confidential.
- **Coverage:** data starts when ingestion started. The harness deletes old transcripts (Claude Code: `cleanupPeriodDays`), so sessions before the first ingest may be missing. Absence is not proof that no work happened.
- **Session duration** is `ended_at - started_at` including idle time: an upper bound, not worked time.
- If results look empty, run `total-recall status --json` to confirm the DB path and that ingestion is current.

## Scope

The archive holds **conversation**, not artifacts. Files, READMEs, design documents and code are in the repositories, where they are faster to read and more current. Use the archive for the why and when: what was asked, what was decided, what was tried, in which session and repo.

## Commands

| Command | Use it for |
|---|---|
| `total-recall status --json` | DB path, size, schema version, counts, last ingest, last backup, errors of the last 24h (`--all-errors` for all, `--errors N` caps the lines) |
| `total-recall sql --json "SELECT …"` | read-only SQL (`mode=ro`, `PRAGMA query_only`); `--format table\|json\|jsonl\|csv`, `--no-truncate` |
| `total-recall sessions --json [--since D] [--until D] [--repo R] [--harness H] [--name N] [--limit N] [--subagents] [--all-sessions]` | sessions newest first; `--name` is a substring of the display name; `--all-sessions` includes sessions without events |
| `total-recall show SESSION --json [--since D] [--until D] [--limit N] [--offset N] [--from-event ID] [--all]` | conversation of one session; without bounds the whole session; `--from-event` starts at the `event_id` of a search hit |
| `total-recall search WORDS --json [filters as sessions] [--actor A] [--session ID] [--raw] [--limit N]` | full-text hits with snippets, best first; all words must match; `--raw` passes FTS5 syntax through |
| `total-recall stats --json --month YYYY-MM` (or `--since/--until`) | totals per day, repo and weekday x hour |
| `total-recall stats --json --period day\|week\|month [--by project\|agent\|directory] [--top N]` | time series per period, optionally per key |
| `total-recall stats --json --workday [--range today\|this-week\|last-week\|this-month\|last-month] [--all-events]` | first and last activity per day with span and target |
| `total-recall doctor --json` | format drift of the transcripts; exit 2 = findings. Not needed for queries |
| `total-recall llm --skill` | prints this document |

Every read command takes `--format table|json|jsonl` (`sql` also `csv`), `--json` and `--no-truncate`. Global flags: `--db PATH`, `--config PATH`, `--tz ZONE` (default `Europe/Berlin`; used by `stats`, day boundaries and displayed times).

### JSON fields

#### `sessions --json`

One object per session.

| field | meaning |
|---|---|
| `id` | column |
| `harness` | column |
| `native_id` | column |
| `parent_id` | column; `null` for top-level sessions |
| `display_name` | generated column |
| `name` | column |
| `ai_title` | column |
| `agent_name` | column |
| `cwd` | column |
| `git_root` | column |
| `git_remote_url` | column |
| `repo` | computed: `git_remote_url`, else `git_root`, else `cwd` |
| `git_branch` | column |
| `model` | column |
| `harness_version` | column |
| `started_at` | column (UTC) |
| `ended_at` | column (UTC) |
| `duration_seconds` | computed: `ended_at - started_at` |
| `human_events` | computed: count of `actor = 'human'` |
| `agent_events` | computed: count of `actor = 'agent'` |
| `events` | computed: count of all events |

#### `show --json`

| field | meaning |
|---|---|
| `session` | the session as in `sessions --json` |
| `events` | array of events, oldest first |

#### `show --json` events[]

| field | meaning |
|---|---|
| `id` | `events.id` |
| `ts` | UTC |
| `kind` | see `events.kind` |
| `role` | see `events.role` |
| `actor` | see `events.actor` |
| `text` | readable text |
| `tool_name` | for `tool_use`, else `null` |
| `model` | `null` for human events |
| `tokens_in` | `null` if unknown |
| `tokens_out` | `null` if unknown |

#### `search --json`

One object per hit.

| field | meaning |
|---|---|
| `session_id` | |
| `session_name` | display name of the session |
| `repo` | as in `sessions --json` |
| `event_id` | `events.id` |
| `ts` | UTC |
| `actor` | |
| `kind` | |
| `snippet` | matched words in `[…]` |
| `rank` | FTS5 rank, lower is better |

#### `status --json`

| field | meaning |
|---|---|
| `db` | path |
| `db_size_bytes` | |
| `schema_version` | version of the database |
| `binary_schema_version` | version the binary supports |
| `sessions` | count |
| `events` | count |
| `sources` | count of ingested files |
| `last_ingest` | object harness → time (UTC) |
| `backup_dir` | |
| `last_backup` | path of the last backup, `null` if none |
| `last_backup_at` | time of the last backup, `null` if none |
| `log` | path of the log file |
| `errors_window` | age limit of `recent_errors`, `"24h"`; `null` with `--all-errors` |
| `errors_since` | start of the window (UTC); `null` with `--all-errors` |
| `recent_errors` | error lines of the window, oldest first, at most `--errors N` (default 5): `at` (UTC, `null` without timestamp), `message`. Lines about transcripts that did not exist yet are never included |
| `recent_errors_total` | count of errors in the window, including lines beyond the cap |
| `older_errors` | count of errors before the window (0 with `--all-errors`) |

#### `stats --json`

| field | meaning |
|---|---|
| `since` | start of the range (UTC) |
| `until` | end of the range (UTC) |
| `timezone` | |
| `total` | `sessions`, `human`, `agent` |
| `by_day` | per local day: `day`, `sessions`, `human`, `agent` |
| `by_repo` | per repo: `repo`, `sessions`, `human`, `agent` |
| `by_weekday_hour` | all 7x24 cells: `weekday` (0 = Monday), `day`, `hour`, `sessions`, `human`, `agent` |

#### `stats --json --period`

| field | meaning |
|---|---|
| `since` | start of the range (UTC) |
| `until` | end of the range (UTC) |
| `timezone` | |
| `period` | `day`, `week` or `month` |
| `by` | `project`, `agent`, `directory` or `null` |
| `keys` | all keys of the range |
| `total` | `sessions`, `human`, `agent` over the range |
| `by_key` | per key: `key`, `sessions`, `human`, `agent` |
| `buckets` | every period of the range: `start` (UTC), `label`, `sessions`, `human`, `agent`, `cells[]` (`key`, `sessions`, `human`, `agent`; only keys with activity) |

#### `stats --json --workday`

| field | meaning |
|---|---|
| `since` | start of the range (UTC) |
| `until` | end of the range (UTC) |
| `timezone` | |
| `day_start` | `HH:MM`; activity before it counts for the previous day |
| `human_only` | whether only the user's own messages were counted |
| `work_hours` | configured core hours |
| `target_seconds` | daily target |
| `holiday_region` | region of the public holidays |
| `days` | per day, see below |
| `total` | see below |

#### `stats --json --workday` days[]

| field | meaning |
|---|---|
| `day` | `YYYY-MM-DD` local |
| `weekday` | `Mon` … `Sun` |
| `first` | first activity (UTC), `null` without activity |
| `last` | last activity (UTC), `null` without activity |
| `first_local` | local clock, `01:30 (+1)` past midnight |
| `last_local` | local clock |
| `span_seconds` | elapsed time between first and last |
| `weekend` | |
| `holiday` | name of the public holiday, else `null` |
| `target_seconds` | daily target, 0 on weekends, holidays and days without activity |
| `diff_seconds` | span minus target |
| `sessions` | |
| `human` | |
| `agent` | |

#### `stats --json --workday` total

| field | meaning |
|---|---|
| `days` | days with activity |
| `workdays` | weekdays that are no holiday, up to today |
| `holidays` | |
| `span_seconds` | |
| `target_seconds` | target x workdays |
| `balance_seconds` | span minus target |
