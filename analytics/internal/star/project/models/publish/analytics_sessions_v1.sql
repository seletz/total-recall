-- Publication layer: the analytics_*_v1 views are the only interface for
-- consumers (notebooks, BI, the commands). Their columns are a contract
-- (contract.txt), checked after every run; columns only grow, a breaking
-- change gets _v2 next to _v1. No text, no full-text search. Times UTC;
-- local days in the primary zone (var tz), other zones via
-- analytics_time_slot_v1.
SELECT s.session_id, h.host_name AS host, hr.harness, hr.harness_version,
       p.project_name AS project,  -- as stats --by project names it
       r.repo,                     -- remote URL, git root or cwd (stats by_repo)
       s.git_branch, m.model,      -- first model of the agent
       ac.account_id, ac.account_name, ac.account_group,
       ps.session_id AS parent_id, s.is_subagent,
       f.start_utc AS started_at, f.end_utc AS ended_at,
       f.duration_s, f.events, f.human_events, f.agent_events, f.tool_calls,
       f.distinct_models, f.model_switches, f.tokens_in, f.tokens_out
FROM {{ ref('fact_session') }} f
LEFT JOIN {{ ref('dim_session') }} s ON s.session_key = f.session_key
LEFT JOIN {{ ref('dim_session') }} ps ON ps.session_key = s.parent_session_key
LEFT JOIN {{ ref('dim_host') }} h ON h.host_key = f.host_key
LEFT JOIN {{ ref('dim_harness') }} hr ON hr.harness_key = f.harness_key
LEFT JOIN {{ ref('dim_project') }} p ON p.project_key = f.project_key
LEFT JOIN {{ ref('dim_repo') }} r ON r.repo_key = f.repo_key
LEFT JOIN {{ ref('dim_model') }} m ON m.model_key = f.first_model_key
LEFT JOIN {{ ref('dim_account') }} ac ON ac.account_key = f.account_key
