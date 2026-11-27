-- Every session of fact_event has exactly its row in fact_session, with the
-- same number of events.
SELECT f.session_key, COUNT(*) AS fact_events, s.events AS session_events
FROM fact_event f LEFT JOIN fact_session s ON s.session_key = f.session_key
GROUP BY f.session_key
HAVING s.session_key IS NULL OR COUNT(*) <> s.events
LIMIT 20
