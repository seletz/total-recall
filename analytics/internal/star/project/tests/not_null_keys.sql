-- No fact row without a key; NULL keys would vanish from every join.
SELECT event_id FROM {{ ref('fact_event') }}
WHERE event_key IS NULL OR host_key IS NULL OR session_key IS NULL OR project_key IS NULL OR repo_key IS NULL
   OR harness_key IS NULL OR account_key IS NULL OR model_key IS NULL OR tool_key IS NULL OR actor_key IS NULL
   OR event_type_key IS NULL OR ts_utc IS NULL OR slot_key IS NULL
