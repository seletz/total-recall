{{ config(materialized='table') }}
-- Calendar per local date of the bridge.
SELECT DISTINCT local_date AS date,
       CAST(year(local_date) AS INTEGER) AS year,
       CAST(month(local_date) AS INTEGER) AS month,
       CAST(isoyear(local_date) AS INTEGER) AS iso_year,
       CAST(week(local_date) AS INTEGER) AS iso_week,
       CAST(isodow(local_date) AS INTEGER) AS iso_weekday,
       isodow(local_date) >= 6 AS is_weekend
FROM {{ ref('bridge_time_slot') }}
