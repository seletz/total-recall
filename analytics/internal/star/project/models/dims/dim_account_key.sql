{{ config(materialized='incremental', unique_key='account_key') }}
-- Login of a session (subagents take their parent's). The IDs are the
-- natural key; name and group come from the configuration (seed).
SELECT DISTINCT tr_key(concat_ws(chr(31), 'account', account_id, organization_id)) AS account_key,
       account_id, organization_id
FROM {{ ref('stg_event') }}
