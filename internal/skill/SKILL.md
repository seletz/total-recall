---
name: total-recall
description: Query the local archive of past coding-agent sessions (Claude Code and others) with the total-recall CLI. Use when the user asks about earlier agent sessions or their work history - what they worked on (in a repo, on a day, in a month), how much time or how many sessions went into something, what was discussed, asked or decided in a previous session, or where a topic came up before.
---

# total-recall

`total-recall` archives coding-agent sessions into a local SQLite database. Hooks feed it incrementally, so it holds transcripts that the harness itself may already have deleted. Use it to answer questions about past sessions instead of guessing or searching raw transcript files.

## What the archive contains

- One row per **session**: id, harness, title, working directory, git repo (remote URL, root, branch), model, start and end time. Subagent sessions point to their parent.
- One row per **event**: every user message, assistant message, tool call and tool result, with timestamp, who wrote it (`actor`) and readable text.
- A full-text index (SQLite FTS5) over event text.
- Views for request/response pairs (`exchanges`) and per-day activity (`daily_activity`).

## Commands

Every read command accepts `--json`. Prefer `--json` and parse the output.

| Command | Use it for |
|---|---|
| `total-recall status --json` | DB path, counts, last ingest, last backup. Check this first if results look empty. |
| `total-recall sessions --json [--since DATE] [--until DATE] [--repo R] [--harness H] [--name N]` | list sessions with start, duration, name, repo, branch, #human, #agent |
| `total-recall show SESSION_ID --json [--all]` | conversation of one session; `--all` includes tool calls/results |
| `total-recall search QUERY --json [filters as sessions] [--actor A]` | FTS5 search with snippets |
| `total-recall stats --json --month YYYY-MM` (or `--since/--until`) | per day, per weekday x hour (local time), per repo: sessions, human and agent events |
| `total-recall sql --json "SELECT ..."` | read-only SQL (table, JSON or CSV output); writes are impossible |
| `total-recall llm --skill` | prints this document |

Global flags: `--db PATH`, `--config PATH`, `--tz ZONE` (default `Europe/Berlin`, used by `stats`, day boundaries and displayed times).

Session ids look like `claude:f2b299a1-b3c1-4cf8-b1d7-8c4840eab467` (`<harness>:<native id>`).

FTS5 query syntax for `search` and `MATCH`: `word`, `"exact phrase"`, `prefix*`, `a AND b`, `a OR b`, `a NOT b`.

## Schema

All timestamps are UTC ISO 8601 TEXT, e.g. `2026-10-31T22:21:58.123Z`.

### `sessions`

| column | meaning |
|---|---|
| `id` | PK, `<harness>:<native id>` |
| `harness` | e.g. `claude` |
| `harness_version` | last seen harness version |
| `native_id` | harness session id |
| `parent_id` | parent session for subagents, else NULL |
| `name` | title (custom title, agent name or slug) |
| `agent_name` | |
| `cwd` | first seen working directory |
| `git_root` | main repo root (worktrees resolved to the main repo) |
| `git_remote_url` | normalized, e.g. `https://github.com/owner/repo`, no credentials |
| `git_branch` | last seen branch |
| `model` | last seen model |
| `started_at`, `ended_at` | first and last event timestamp |

### `events`

| column | meaning |
|---|---|
| `id` | PK |
| `session_id` | FK to `sessions.id` |
| `ts` | timestamp |
| `kind` | `message`, `tool_use`, `tool_result`, `system`, `meta` |
| `role` | `user`, `assistant`, `system`, `tool` |
| `actor` | `human`, `agent`, `peer`, `automation`, `tool` (see caveats) |
| `text` | readable, redacted text; tool results truncated to 4 KiB |
| `tool_name` | for `tool_use` |
| `model` | |
| `tokens_in`, `tokens_out` | may be NULL |
| `source_path`, `source_offset` | where the line came from |
| `raw` | original JSON line, redacted |

### Other tables and views

- `events_fts`: FTS5 over `events.text` (external content; join on `events.rowid = events_fts.rowid`).
- `exchanges` (view): one row per human message with the request and the last agent reply before the next human message. Columns (check with `sqlite_master` if unsure): `session_id`, `ts`, `request`, `response`.
- `daily_activity` (view): day in local time (Europe/Berlin), session, repo, count of human and agent events.
- `sources`: ingested files with byte offset and ingest time.
- `dirs`: cache of `cwd` -> `git_root`, `git_remote_url`.

If a column name in this document does not match, inspect the real schema:

```sh
total-recall sql "SELECT name, sql FROM sqlite_master WHERE type IN ('table','view')"
```

## Example queries

Run each with `total-recall sql --json "<query>"`.

Sessions per repo per month (top-level sessions only):

```sql
SELECT strftime('%Y-%m', started_at, 'localtime') AS month,
       coalesce(git_remote_url, cwd) AS repo,
       count(*) AS sessions
FROM sessions
WHERE parent_id IS NULL
GROUP BY month, repo
ORDER BY month DESC, sessions DESC;
```

Human messages per day (last 30 days with activity):

```sql
SELECT date(ts, 'localtime') AS day, count(*) AS human_messages
FROM events
WHERE actor = 'human' AND kind = 'message'
GROUP BY day
ORDER BY day DESC
LIMIT 30;
```

Sessions mentioning a topic, most recent first:

```sql
SELECT s.id, s.name, s.git_remote_url, s.started_at, count(*) AS hits
FROM events_fts f
JOIN events e ON e.rowid = f.rowid
JOIN sessions s ON s.id = e.session_id
WHERE events_fts MATCH 'goreleaser'
GROUP BY s.id
ORDER BY s.started_at DESC
LIMIT 20;
```

What the human said about a topic (with snippet):

```sql
SELECT e.session_id, e.ts, snippet(events_fts, 0, '[', ']', '...', 12) AS snippet
FROM events_fts
JOIN events e ON e.rowid = events_fts.rowid
WHERE events_fts MATCH '"backup retention"' AND e.actor = 'human'
ORDER BY e.ts DESC
LIMIT 20;
```

Request/response pairs of one session:

```sql
SELECT ts, request, response
FROM exchanges
WHERE session_id = 'claude:f2b299a1-b3c1-4cf8-b1d7-8c4840eab467'
ORDER BY ts;
```

Sessions for a git remote:

```sql
SELECT id, name, git_branch, started_at, ended_at
FROM sessions
WHERE git_remote_url = 'https://github.com/seletz/total-recall'
ORDER BY started_at DESC;
```

Busiest hours of the day (local time):

```sql
SELECT strftime('%H', ts, 'localtime') AS hour, count(*) AS human_messages
FROM events
WHERE actor = 'human' AND kind = 'message'
GROUP BY hour
ORDER BY human_messages DESC;
```

Approximate time per repo in a month (sum of session spans, minutes):

```sql
SELECT coalesce(git_remote_url, cwd) AS repo,
       count(*) AS sessions,
       round(sum(julianday(ended_at) - julianday(started_at)) * 24 * 60) AS minutes
FROM sessions
WHERE parent_id IS NULL
  AND started_at >= '2026-10-01' AND started_at < '2026-11-01'
GROUP BY repo
ORDER BY minutes DESC;
```

Most used tools:

```sql
SELECT tool_name, count(*) AS calls
FROM events
WHERE kind = 'tool_use'
GROUP BY tool_name
ORDER BY calls DESC
LIMIT 15;
```

Sessions on a given local day:

```sql
SELECT id, name, git_remote_url, started_at, ended_at
FROM sessions
WHERE date(started_at, 'localtime') = '2026-10-31'
ORDER BY started_at;
```

## Caveats

- **Times are UTC in the database.** Convert with `datetime(ts, 'localtime')` / `date(ts, 'localtime')` (uses the machine's time zone) or use `stats` and `daily_activity`, which use the configured zone (default Europe/Berlin). Date literals compared against `ts`/`started_at` are UTC.
- **Actors:** `human` = typed by the user (including `!cmd` bash input and pasted content); `agent` = the assistant; `peer` = another agent session or a subagent hand-back; `automation` = scheduled prompts, system reminders, slash-command plumbing, continuation summaries; `tool` = tool results. For "what did the user ask" filter on `actor = 'human'`; for counting real work, ignore `automation`.
- **Session duration** is `ended_at - started_at` and includes idle time. Treat it as an upper bound, not as worked time.
- **Subagents** are separate sessions with `parent_id` set. Exclude them (`parent_id IS NULL`) when counting sessions.
- **Redaction:** secrets were masked before storage as `«redacted:<kind>»`. The originals are gone; do not try to recover them. Redaction is heuristic, so still treat the content as confidential.
- **Coverage:** data only starts when ingestion started. The harness deletes old transcripts (Claude Code: `cleanupPeriodDays`), so sessions older than the first ingest may be missing. Absence of a session is not proof that no work happened.
- **Tool results** are truncated to 4 KiB in `text`.
- `sql` is read-only. Do not attempt writes; use `LIMIT` on exploratory queries.
- If commands return nothing, run `total-recall status --json` to confirm the DB path and that ingestion is working.
