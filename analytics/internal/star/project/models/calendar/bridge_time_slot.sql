{{ config(materialized='table') }}
-- Time-zone bridge: one row per zone (var zones, the primary zone first)
-- and UTC quarter hour of the facts (slot_key = unix seconds / 900). Every
-- real zone offset is a multiple of 15 minutes, so local day and hour
-- boundaries are exact. Facts hold UTC only; a query picks the zone.
WITH r AS (
	SELECT min(slot_key) AS lo, max(slot_key) AS hi FROM {{ ref('fact_event') }}
), z AS (
	SELECT unnest(string_split('{{ var('zones') }}', ',')) AS tz
), s AS (
	SELECT unnest(generate_series(lo, hi)) AS slot_key FROM r WHERE lo IS NOT NULL
), l AS (
	SELECT z.tz, s.slot_key, timezone(z.tz, to_timestamp(s.slot_key * 900)) AS local_ts
	FROM z CROSS JOIN s
)
SELECT tz, slot_key, CAST(local_ts AS DATE) AS local_date, CAST(hour(local_ts) AS INTEGER) AS local_hour,
       CAST(isodow(local_ts) AS INTEGER) AS iso_weekday
FROM l
