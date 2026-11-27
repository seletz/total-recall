-- Transform, dimensions: upsert every natural key of the batch. Nothing is
-- ever deleted (accumulating superset), attributes are overwritten (SCD 1).
-- Project names and login names come from the configuration and are set
-- by Go after this step (etl.go: nameProjects).

INSERT INTO dim_host (host_key, host_name)
SELECT DISTINCT tr_key('host', host), host FROM stg_event WHERE true
ON CONFLICT (host_key) DO NOTHING;

INSERT INTO dim_harness (harness_key, harness, harness_version)
SELECT DISTINCT tr_key('harness', harness, harness_version), harness, harness_version FROM stg_event WHERE true
ON CONFLICT (harness_key) DO NOTHING;

INSERT INTO dim_model (model_key, model, model_family)
SELECT DISTINCT tr_key('model', model), model,
       CASE WHEN model = '' THEN 'unknown'
            WHEN model LIKE '%opus%' THEN 'opus'
            WHEN model LIKE '%sonnet%' THEN 'sonnet'
            WHEN model LIKE '%haiku%' THEN 'haiku'
            ELSE 'other' END
FROM stg_event WHERE true
ON CONFLICT (model_key) DO UPDATE SET model_family = excluded.model_family;

INSERT INTO dim_tool (tool_key, tool_name, tool_kind, mcp_server)
SELECT DISTINCT tr_key('tool', tool_name), tool_name,
       CASE WHEN tool_name = '' THEN 'none' WHEN tool_name LIKE 'mcp\_\_%' ESCAPE '\' THEN 'mcp' ELSE 'builtin' END,
       CASE WHEN tool_name LIKE 'mcp\_\_%\_\_%' ESCAPE '\'
            THEN substr(tool_name, 6, instr(substr(tool_name, 6), '__') - 1) ELSE '' END
FROM stg_event WHERE true
ON CONFLICT (tool_key) DO UPDATE SET tool_kind = excluded.tool_kind, mcp_server = excluded.mcp_server;

INSERT INTO dim_actor (actor_key, actor, is_human)
SELECT DISTINCT tr_key('actor', actor), actor, actor = 'human' FROM stg_event WHERE true
ON CONFLICT (actor_key) DO NOTHING;

INSERT INTO dim_event_type (event_type_key, kind, role, input_kind)
SELECT DISTINCT tr_key('event_type', kind, role, input_kind), kind, role, input_kind FROM stg_event WHERE true
ON CONFLICT (event_type_key) DO NOTHING;

INSERT INTO dim_account (account_key, account_id, organization_id, account_name, account_group)
SELECT DISTINCT tr_key('account', account_id, organization_id), account_id, organization_id,
       CASE WHEN account_id = '' AND organization_id = '' THEN 'unknown' ELSE '' END, ''
FROM stg_event WHERE true
ON CONFLICT (account_key) DO NOTHING;

INSERT INTO dim_project (project_key, project_path, is_dir, remote_url, project_name)
SELECT tr_key('project', project_path), project_path, MAX(is_dir), MAX(remote_url), ''
FROM stg_event GROUP BY project_path
ON CONFLICT (project_key) DO UPDATE SET is_dir = excluded.is_dir, remote_url = excluded.remote_url;

INSERT INTO dim_repo (repo_key, repo)
SELECT DISTINCT tr_key('repo', repo), repo FROM stg_event WHERE true
ON CONFLICT (repo_key) DO NOTHING;

-- Parent and continuation keys only when the target is archived; a hash of
-- an unarchived ID would be a dangling key.
INSERT INTO dim_session (session_key, session_id, parent_session_key, continued_in_key, is_subagent, git_branch)
SELECT tr_key('session', x.session_id), x.session_id,
       CASE WHEN EXISTS (SELECT 1 FROM src.sessions p WHERE p.id = x.parent_id) THEN tr_key('session', x.parent_id) END,
       CASE WHEN EXISTS (SELECT 1 FROM src.sessions c WHERE c.id = x.continued_in) THEN tr_key('session', x.continued_in) END,
       x.parent_id IS NOT NULL AND x.parent_id <> '',
       x.git_branch
FROM (SELECT DISTINCT session_id, parent_id, continued_in, git_branch FROM stg_event) x
WHERE true
ON CONFLICT (session_key) DO UPDATE SET
	parent_session_key = excluded.parent_session_key,
	continued_in_key = excluded.continued_in_key,
	is_subagent = excluded.is_subagent,
	git_branch = excluded.git_branch;
