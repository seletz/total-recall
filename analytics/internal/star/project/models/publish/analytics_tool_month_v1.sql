SELECT month, project_name AS project, tool_name, tool_kind, mcp_server, calls
FROM {{ ref('mart_tool_month') }}
