-- Every fact slot has a bridge row in every zone of the bridge; a missing
-- one would drop the event from all local-time marts.
SELECT z.tz, f.slot_key
FROM (SELECT DISTINCT slot_key FROM {{ ref('fact_event') }}) f
CROSS JOIN (SELECT DISTINCT tz FROM {{ ref('bridge_time_slot') }}) z
WHERE NOT EXISTS (SELECT 1 FROM {{ ref('bridge_time_slot') }} b WHERE b.tz = z.tz AND b.slot_key = f.slot_key)
