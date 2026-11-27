-- The sessions of a working day and category add up to the day's credited
-- time, and every slice belongs to a loaded session.
SELECT d.local_date, d.category, d.raw_s, COALESCE(sum(w.raw_s), 0) AS slices
FROM {{ ref('fact_work_day') }} d
LEFT JOIN {{ ref('fact_work_slice') }} w ON w.local_date = d.local_date AND w.category = d.category
GROUP BY d.local_date, d.category, d.raw_s
HAVING d.raw_s <> COALESCE(sum(w.raw_s), 0)
UNION ALL
SELECT w.local_date, w.category, w.raw_s, 0 FROM {{ ref('fact_work_slice') }} w
WHERE NOT EXISTS (SELECT 1 FROM {{ ref('fact_session') }} f WHERE f.session_key = w.session_key)
