-- The input splits without remainder: fresh + cache read + cache write +
-- unsplit = input, for every event.
SELECT event_id FROM {{ ref('fact_event') }}
WHERE tokens_in <> tokens_fresh + tokens_cache_read + tokens_cache_write + tokens_unsplit
