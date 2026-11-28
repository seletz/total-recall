{{ config(materialized='table') }}
-- Extract: the events of every session that has an event above the
-- watermark (var seq_from), with the session attributes resolved as the
-- archive's own statistics resolve them. A whole touched session is
-- restaged, so session attributes that changed since the last load (git
-- root resolved, login recorded, parent archived later) reach all its
-- events. No text: message text, titles, tool arguments and raw lines stay
-- in the archive. Times: UTC.
WITH touched AS (
	SELECT DISTINCT session_id FROM {{ source('tr', 'events') }} WHERE seq > {{ var('seq_from') }}
), sess AS (
	SELECT s.id AS session_id,
	       s.harness,
	       COALESCE(s.harness_version, '') AS harness_version,
	       COALESCE(s.git_branch, '') AS git_branch,
	       COALESCE(s.parent_id, '') AS parent_id,
	       COALESCE(s.continued_in, '') AS continued_in,
	       -- repo: as stats by_repo, the session's own values
	       COALESCE(NULLIF(s.git_remote_url, ''), NULLIF(s.git_root, ''), s.cwd, '') AS repo,
	       -- project: as stats --by project, subagents take the parent's git
	       -- root or working directory
	       CASE WHEN COALESCE(p.git_root, '') <> '' OR COALESCE(p.cwd, '') <> ''
	            THEN COALESCE(NULLIF(p.git_root, ''), p.cwd)
	            ELSE COALESCE(NULLIF(s.git_root, ''), s.cwd, '') END AS project_raw,
	       CASE WHEN COALESCE(p.git_root, '') <> '' OR COALESCE(p.cwd, '') <> ''
	            THEN COALESCE(p.git_root, '') = ''
	            ELSE COALESCE(s.git_root, '') = '' END AS is_dir,
	       CASE WHEN COALESCE(p.git_root, '') <> '' OR COALESCE(p.cwd, '') <> ''
	            THEN COALESCE(p.git_remote_url, '')
	            ELSE COALESCE(s.git_remote_url, '') END AS remote_raw,
	       -- login: subagents take the parent's
	       CASE WHEN COALESCE(s.account_id, '') = '' AND COALESCE(s.organization_id, '') = ''
	            THEN COALESCE(p.account_id, '') ELSE COALESCE(s.account_id, '') END AS account_id,
	       CASE WHEN COALESCE(s.account_id, '') = '' AND COALESCE(s.organization_id, '') = ''
	            THEN COALESCE(p.organization_id, '') ELSE COALESCE(s.organization_id, '') END AS organization_id,
	       EXISTS (SELECT 1 FROM {{ source('tr', 'sessions') }} x WHERE x.id = s.parent_id) AS parent_archived,
	       EXISTS (SELECT 1 FROM {{ source('tr', 'sessions') }} x WHERE x.id = s.continued_in) AS continued_archived
	FROM {{ source('tr', 'sessions') }} s
	JOIN touched t ON t.session_id = s.id
	LEFT JOIN {{ source('tr', 'sessions') }} p ON p.id = s.parent_id
), ev AS (
	SELECT e.*, TRY_CAST(e.ts AS TIMESTAMPTZ) AS ts_tz
	FROM {{ source('tr', 'events') }} e
	JOIN touched t ON t.session_id = e.session_id
)
SELECT e.seq,
       e.id AS event_id,
       e.session_id,
       timezone('UTC', e.ts_tz) AS ts_utc,
       CAST(floor(epoch(e.ts_tz) / 900) AS BIGINT) AS slot_key,
       e.kind,
       COALESCE(e.role, '') AS role,
       COALESCE(e.actor, '') AS actor,
       COALESCE(e.input_kind, '') AS input_kind,
       COALESCE(e.tool_name, '') AS tool_name,
       COALESCE(e.model, '') AS model,
       CAST(e.tokens_in AS BIGINT) AS tokens_in,
       CAST(e.tokens_out AS BIGINT) AS tokens_out,
       CAST(e.tokens_cache_read AS BIGINT) AS tokens_cache_read,
       CAST(e.tokens_cache_write AS BIGINT) AS tokens_cache_write,
       '{{ var('source_name') }}' AS source_name,
       '{{ var('host') }}' AS host,
       x.harness, x.harness_version, x.git_branch, x.parent_id, x.continued_in,
       x.parent_archived, x.continued_archived,
       x.repo,
       tr_tilde(x.project_raw, '{{ var('home') }}') AS project_path,
       x.is_dir,
       CASE WHEN x.is_dir THEN '' ELSE x.remote_raw END AS remote_url,
       x.account_id, x.organization_id
FROM ev e
JOIN sess x ON x.session_id = e.session_id
WHERE e.ts_tz IS NOT NULL
