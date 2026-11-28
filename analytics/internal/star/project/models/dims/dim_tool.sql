{{ config(materialized='incremental', unique_key='tool_key') }}
-- tool_name '' = no tool (messages). MCP tools are named mcp__<server>__<tool>.
SELECT DISTINCT tr_key(concat_ws(chr(31), 'tool', tool_name)) AS tool_key, tool_name,
       CASE WHEN tool_name = '' THEN 'none' WHEN starts_with(tool_name, 'mcp__') THEN 'mcp' ELSE 'builtin' END AS tool_kind,
       CASE WHEN starts_with(tool_name, 'mcp__') THEN split_part(tool_name, '__', 2) ELSE '' END AS mcp_server
FROM {{ ref('stg_event') }}
