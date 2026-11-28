{{ config(materialized='incremental', unique_key='project_key') }}
-- Project as stats --by project sees it: the raw key is the git root of the
-- session (subagents: of the parent), else its working directory, with the
-- home directory as "~". The name is not stored here: it depends on the
-- configuration and on all other keys, so Go computes it on every run into
-- seed_project_name and dim_project joins it (SCD 1).
SELECT tr_key(concat_ws(chr(31), 'project', project_path)) AS project_key, project_path,
       bool_or(is_dir) AS is_dir, max(remote_url) AS remote_url
FROM {{ ref('stg_event') }}
GROUP BY project_path
