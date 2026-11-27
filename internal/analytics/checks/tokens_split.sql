-- The input splits without remainder: fresh + cache read + cache write +
-- unsplit = input, for every event.
SELECT event_id, tokens_in, tokens_fresh, tokens_cache_read, tokens_cache_write, tokens_unsplit
FROM fact_event
WHERE tokens_in <> tokens_fresh + tokens_cache_read + tokens_cache_write + tokens_unsplit
LIMIT 20
