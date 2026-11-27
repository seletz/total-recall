{{ config(materialized='table') }}
-- Events with token usage per local day and model.
SELECT '{{ var('tz') }}' AS tz, local_date, COALESCE(model, '') AS model, COALESCE(model_family, 'unknown') AS model_family,
       count(*) AS events, CAST(sum(tokens_in) AS BIGINT) AS tokens_in, CAST(sum(tokens_fresh) AS BIGINT) AS tokens_fresh,
       CAST(sum(tokens_cache_read) AS BIGINT) AS tokens_cache_read, CAST(sum(tokens_cache_write) AS BIGINT) AS tokens_cache_write,
       CAST(sum(tokens_unsplit) AS BIGINT) AS tokens_unsplit, CAST(sum(tokens_out) AS BIGINT) AS tokens_out
FROM {{ ref('int_event_local') }}
WHERE has_usage
GROUP BY local_date, COALESCE(model, ''), COALESCE(model_family, 'unknown')
