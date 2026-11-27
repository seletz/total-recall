-- Closed value sets of the archive (design.md: Actor, events.kind).
SELECT 'actor' AS col, actor AS value FROM dim_actor WHERE actor NOT IN ('human', 'agent', 'peer', 'automation', 'tool', '')
UNION ALL SELECT 'kind', kind FROM dim_event_type WHERE kind NOT IN ('message', 'tool_use', 'tool_result')
UNION ALL SELECT 'input_kind', input_kind FROM dim_event_type WHERE input_kind NOT IN ('typed', 'pasted', 'bash', 'remote', '')
UNION ALL SELECT 'model_family', model_family FROM dim_model WHERE model_family NOT IN ('opus', 'sonnet', 'haiku', 'other', 'unknown')
