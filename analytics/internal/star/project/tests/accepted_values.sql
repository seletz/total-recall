-- Closed value sets of the archive (internal/model: Actor, Kind,
-- InputKind; star/values.go), plus 'unknown', which stg_event writes for a
-- value outside them.
SELECT 'actor' AS col, actor AS value FROM {{ ref('dim_actor') }} WHERE actor NOT IN ('human', 'agent', 'peer', 'automation', 'tool', '', 'unknown')
UNION ALL SELECT 'kind', kind FROM {{ ref('dim_event_type') }} WHERE kind NOT IN ('message', 'tool_use', 'tool_result', 'system', 'meta', 'unknown')
UNION ALL SELECT 'input_kind', input_kind FROM {{ ref('dim_event_type') }} WHERE input_kind NOT IN ('typed', 'pasted', 'bash', 'remote', '', 'unknown')
UNION ALL SELECT 'model_family', model_family FROM {{ ref('dim_model') }} WHERE model_family NOT IN ('opus', 'sonnet', 'haiku', 'other', 'unknown')
UNION ALL SELECT 'tool_kind', tool_kind FROM {{ ref('dim_tool') }} WHERE tool_kind NOT IN ('none', 'builtin', 'mcp')
