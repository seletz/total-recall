{{ config(materialized='incremental', unique_key='host_key') }}
-- Hosts of the pool. Dimensions are accumulating supersets: a run upserts
-- the keys it sees and never deletes keys that no longer occur, so frozen
-- keys of older facts stay valid. Attributes are overwritten (SCD 1).
SELECT DISTINCT tr_key(concat_ws(chr(31), 'host', host)) AS host_key, host AS host_name
FROM {{ ref('stg_event') }}
