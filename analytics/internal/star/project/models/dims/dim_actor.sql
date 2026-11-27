{{ config(materialized='incremental', unique_key='actor_key') }}
SELECT DISTINCT tr_key(concat_ws(chr(31), 'actor', actor)) AS actor_key, actor, actor = 'human' AS is_human
FROM {{ ref('stg_event') }}
