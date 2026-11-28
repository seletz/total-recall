-- Every key of fact_event resolves in its dimension (joins are LEFT in the
-- views, so a dangling key would silently show as NULL).
SELECT 'host' AS dim, event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_host') }} d WHERE d.host_key = f.host_key)
UNION ALL SELECT 'session', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_session') }} d WHERE d.session_key = f.session_key)
UNION ALL SELECT 'project', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_project') }} d WHERE d.project_key = f.project_key)
UNION ALL SELECT 'repo', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_repo') }} d WHERE d.repo_key = f.repo_key)
UNION ALL SELECT 'harness', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_harness') }} d WHERE d.harness_key = f.harness_key)
UNION ALL SELECT 'account', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_account') }} d WHERE d.account_key = f.account_key)
UNION ALL SELECT 'model', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_model') }} d WHERE d.model_key = f.model_key)
UNION ALL SELECT 'tool', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_tool') }} d WHERE d.tool_key = f.tool_key)
UNION ALL SELECT 'actor', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_actor') }} d WHERE d.actor_key = f.actor_key)
UNION ALL SELECT 'event_type', event_id FROM {{ ref('fact_event') }} f WHERE NOT EXISTS (SELECT 1 FROM {{ ref('dim_event_type') }} d WHERE d.event_type_key = f.event_type_key)
