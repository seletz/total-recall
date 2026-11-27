{{ config(materialized='incremental', unique_key='repo_key') }}
-- Repository as stats by_repo sees it: remote URL, else git root, else cwd
-- of the session itself.
SELECT DISTINCT tr_key(concat_ws(chr(31), 'repo', repo)) AS repo_key, repo
FROM {{ ref('stg_event') }}
