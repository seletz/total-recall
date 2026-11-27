-- Every surrogate key is unique in its table, and so is every natural key
-- (a hash collision would show here).
SELECT 'dim_host' AS t, host_key AS k FROM {{ ref('dim_host') }} GROUP BY host_key HAVING count(*) > 1
UNION ALL SELECT 'dim_host name', min(host_key) FROM {{ ref('dim_host') }} GROUP BY host_name HAVING count(*) > 1
UNION ALL SELECT 'dim_harness', harness_key FROM {{ ref('dim_harness') }} GROUP BY harness_key HAVING count(*) > 1
UNION ALL SELECT 'dim_model', model_key FROM {{ ref('dim_model') }} GROUP BY model_key HAVING count(*) > 1
UNION ALL SELECT 'dim_tool', tool_key FROM {{ ref('dim_tool') }} GROUP BY tool_key HAVING count(*) > 1
UNION ALL SELECT 'dim_actor', actor_key FROM {{ ref('dim_actor') }} GROUP BY actor_key HAVING count(*) > 1
UNION ALL SELECT 'dim_event_type', event_type_key FROM {{ ref('dim_event_type') }} GROUP BY event_type_key HAVING count(*) > 1
UNION ALL SELECT 'dim_repo', repo_key FROM {{ ref('dim_repo') }} GROUP BY repo_key HAVING count(*) > 1
UNION ALL SELECT 'dim_session', session_key FROM {{ ref('dim_session') }} GROUP BY session_key HAVING count(*) > 1
UNION ALL SELECT 'dim_session id', min(session_key) FROM {{ ref('dim_session') }} GROUP BY session_id HAVING count(*) > 1
UNION ALL SELECT 'dim_project', project_key FROM {{ ref('dim_project') }} GROUP BY project_key HAVING count(*) > 1
UNION ALL SELECT 'dim_account', account_key FROM {{ ref('dim_account') }} GROUP BY account_key HAVING count(*) > 1
UNION ALL SELECT 'fact_event', event_key FROM {{ ref('fact_event') }} GROUP BY event_key HAVING count(*) > 1
UNION ALL SELECT 'fact_event id', min(event_key) FROM {{ ref('fact_event') }} GROUP BY event_id HAVING count(*) > 1
UNION ALL SELECT 'fact_session', session_key FROM {{ ref('fact_session') }} GROUP BY session_key HAVING count(*) > 1
