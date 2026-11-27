-- Work time per working day and category: credited and rounded seconds as
-- stats --worktime books them.
SELECT local_date AS day_local, category, raw_s AS raw_seconds, rounded_s AS rounded_seconds
FROM {{ ref('fact_work_day') }}
