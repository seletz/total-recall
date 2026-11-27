{{ config(materialized='incremental', unique_key='model_key') }}
-- model '' = unknown (human events, tool results).
SELECT DISTINCT tr_key(concat_ws(chr(31), 'model', model)) AS model_key, model,
       CASE WHEN model = '' THEN 'unknown'
            WHEN model LIKE '%opus%' THEN 'opus'
            WHEN model LIKE '%sonnet%' THEN 'sonnet'
            WHEN model LIKE '%haiku%' THEN 'haiku'
            ELSE 'other' END AS model_family
FROM {{ ref('stg_event') }}
