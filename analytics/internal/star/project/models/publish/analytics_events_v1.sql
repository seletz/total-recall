SELECT f.event_id, s.session_id, h.host_name AS host, p.project_name AS project, r.repo,
       s.is_subagent, f.ts_utc AS ts, b.local_date AS day_local, b.local_hour AS hour_local,
       b.iso_weekday AS weekday_local, f.slot_key,
       et.kind, et.role, a.actor, et.input_kind, t.tool_name, t.tool_kind, t.mcp_server,
       m.model, m.model_family, hr.harness, ac.account_name, ac.account_group,
       f.tokens_in, f.tokens_out, f.tokens_cache_read, f.tokens_cache_write,
       f.tokens_fresh, f.tokens_unsplit
FROM {{ ref('fact_event') }} f
LEFT JOIN {{ ref('bridge_time_slot') }} b ON b.slot_key = f.slot_key AND b.tz = '{{ var('tz') }}'
LEFT JOIN {{ ref('dim_session') }} s ON s.session_key = f.session_key
LEFT JOIN {{ ref('dim_host') }} h ON h.host_key = f.host_key
LEFT JOIN {{ ref('dim_project') }} p ON p.project_key = f.project_key
LEFT JOIN {{ ref('dim_repo') }} r ON r.repo_key = f.repo_key
LEFT JOIN {{ ref('dim_harness') }} hr ON hr.harness_key = f.harness_key
LEFT JOIN {{ ref('dim_account') }} ac ON ac.account_key = f.account_key
LEFT JOIN {{ ref('dim_model') }} m ON m.model_key = f.model_key
LEFT JOIN {{ ref('dim_tool') }} t ON t.tool_key = f.tool_key
LEFT JOIN {{ ref('dim_actor') }} a ON a.actor_key = f.actor_key
LEFT JOIN {{ ref('dim_event_type') }} et ON et.event_type_key = f.event_type_key
