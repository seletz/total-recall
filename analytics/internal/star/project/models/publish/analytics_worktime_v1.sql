-- Work time per working day, category and session (fact_work_slice), with
-- the session's project and host: the credited seconds of stats --worktime,
-- computed over all hosts together (a gap belongs to the person).
SELECT w.local_date AS day_local, w.category, p.project_name AS project, h.host_name AS host,
       s.session_id, w.raw_s AS raw_seconds
FROM {{ ref('fact_work_slice') }} w
LEFT JOIN {{ ref('dim_session') }} s ON s.session_key = w.session_key
LEFT JOIN {{ ref('fact_session') }} f ON f.session_key = w.session_key
LEFT JOIN {{ ref('dim_project') }} p ON p.project_key = f.project_key
LEFT JOIN {{ ref('dim_host') }} h ON h.host_key = f.host_key
