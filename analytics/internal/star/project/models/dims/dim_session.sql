{{ config(materialized='incremental', unique_key='session_key') }}
-- Parent and continuation keys only when the target session is archived; a
-- hash of an unarchived ID would be a dangling key.
SELECT DISTINCT tr_key(concat_ws(chr(31), 'session', session_id)) AS session_key, session_id,
       CASE WHEN parent_archived THEN tr_key(concat_ws(chr(31), 'session', parent_id)) END AS parent_session_key,
       CASE WHEN continued_archived THEN tr_key(concat_ws(chr(31), 'session', continued_in)) END AS continued_in_key,
       parent_id <> '' AS is_subagent,
       git_branch
FROM {{ ref('stg_event') }}
