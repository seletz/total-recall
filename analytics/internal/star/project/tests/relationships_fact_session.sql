-- Every session of fact_event has exactly its row in fact_session, with the
-- same number of events, and fact_session has no session without events.
SELECT f.session_key, count(*) AS fact_events, max(s.events) AS session_events
FROM {{ ref('fact_event') }} f LEFT JOIN {{ ref('fact_session') }} s ON s.session_key = f.session_key
GROUP BY f.session_key
HAVING max(s.session_key) IS NULL OR count(*) <> max(s.events)
UNION ALL
SELECT s.session_key, 0, s.events FROM {{ ref('fact_session') }} s
WHERE NOT EXISTS (SELECT 1 FROM {{ ref('fact_event') }} f WHERE f.session_key = s.session_key)
