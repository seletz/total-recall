SELECT local_date AS day_local, model, model_family, events, tokens_in, tokens_fresh,
       tokens_cache_read, tokens_cache_write, tokens_unsplit, tokens_out
FROM {{ ref('mart_tokens_day') }}
