{{ config(materialized='table') }}
SELECT k.project_key, k.project_path, k.is_dir, k.remote_url,
       COALESCE(n.project_name, k.project_path) AS project_name
FROM {{ ref('dim_project_key') }} k
LEFT JOIN {{ ref('seed_project_name') }} n ON n.project_key = k.project_key
