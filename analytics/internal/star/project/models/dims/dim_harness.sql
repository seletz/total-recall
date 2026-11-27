{{ config(materialized='incremental', unique_key='harness_key') }}
SELECT DISTINCT tr_key(concat_ws(chr(31), 'harness', harness, harness_version)) AS harness_key,
       harness, harness_version
FROM {{ ref('stg_event') }}
