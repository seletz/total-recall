-- Extract: the events of every session that has an event above the
-- watermark (:seq_from), with the session attributes resolved as the
-- archive's own statistics resolve them. A whole touched session is
-- restaged, so session attributes that changed since the last load (git
-- root resolved, login recorded, parent archived later) reach all its
-- events. Parameters: :seq_from, :home, :host (the host when the source
-- has no sessions.host column, #347 migration v11; {{host_expr}} is set by
-- etl.go after a look at the source's columns).
DROP TABLE IF EXISTS temp.stg_event;
CREATE TEMP TABLE stg_event AS
WITH touched AS (
	SELECT DISTINCT session_id FROM src.events WHERE seq > :seq_from
), sess AS (
	SELECT s.id AS session_id,
	       {{host_expr}} AS host,
	       s.harness,
	       COALESCE(s.harness_version, '') AS harness_version,
	       COALESCE(s.git_branch, '') AS git_branch,
	       s.parent_id,
	       s.continued_in,
	       -- repo: as stats by_repo (store.repoExpr), the session's own values
	       COALESCE(NULLIF(s.git_remote_url, ''), NULLIF(s.git_root, ''), s.cwd, '') AS repo,
	       -- project: as stats --by project (store.activityKey), subagents
	       -- take the parent's git root or working directory
	       CASE WHEN COALESCE(p.git_root, '') <> '' OR COALESCE(p.cwd, '') <> ''
	            THEN COALESCE(NULLIF(p.git_root, ''), p.cwd)
	            ELSE COALESCE(NULLIF(s.git_root, ''), s.cwd, '') END AS project_raw,
	       CASE WHEN COALESCE(p.git_root, '') <> '' OR COALESCE(p.cwd, '') <> ''
	            THEN COALESCE(p.git_root, '') = ''
	            ELSE COALESCE(s.git_root, '') = '' END AS is_dir,
	       CASE WHEN COALESCE(p.git_root, '') <> '' OR COALESCE(p.cwd, '') <> ''
	            THEN COALESCE(p.git_remote_url, '')
	            ELSE COALESCE(s.git_remote_url, '') END AS remote_raw,
	       -- login: as store.ownerExprs, subagents take the parent's
	       CASE WHEN COALESCE(s.account_id, '') = '' AND COALESCE(s.organization_id, '') = ''
	            THEN COALESCE(p.account_id, '') ELSE COALESCE(s.account_id, '') END AS account_id,
	       CASE WHEN COALESCE(s.account_id, '') = '' AND COALESCE(s.organization_id, '') = ''
	            THEN COALESCE(p.organization_id, '') ELSE COALESCE(s.organization_id, '') END AS organization_id
	FROM src.sessions s
	JOIN touched t ON t.session_id = s.id
	LEFT JOIN src.sessions p ON p.id = s.parent_id
)
SELECT e.seq,
       e.id AS event_id,
       e.session_id,
       e.ts,
       CAST(strftime('%s', e.ts) AS INTEGER) / 900 AS slot_key,
       e.kind,
       COALESCE(e.role, '') AS role,
       COALESCE(e.actor, '') AS actor,
       COALESCE(e.input_kind, '') AS input_kind,
       COALESCE(e.tool_name, '') AS tool_name,
       COALESCE(e.model, '') AS model,
       e.tokens_in, e.tokens_out, e.tokens_cache_read, e.tokens_cache_write,
       x.host, x.harness, x.harness_version, x.git_branch, x.parent_id, x.continued_in,
       x.repo,
       tr_tilde(x.project_raw, :home) AS project_path,
       x.is_dir,
       CASE WHEN x.is_dir THEN '' ELSE x.remote_raw END AS remote_url,
       x.account_id, x.organization_id
FROM src.events e
JOIN sess x ON x.session_id = e.session_id
WHERE strftime('%s', e.ts) IS NOT NULL;
