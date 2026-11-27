{{ config(materialized='incremental', unique_key='event_type_key') }}
-- Junk dimension: the small, low-cardinality event attributes.
SELECT DISTINCT tr_key(concat_ws(chr(31), 'event_type', kind, role, input_kind)) AS event_type_key,
       kind, role, input_kind
FROM {{ ref('stg_event') }}
