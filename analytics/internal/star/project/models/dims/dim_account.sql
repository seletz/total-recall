{{ config(materialized='table') }}
SELECT k.account_key, k.account_id, k.organization_id,
       COALESCE(n.account_name, CASE WHEN k.account_id = '' AND k.organization_id = '' THEN 'unknown' ELSE '' END) AS account_name,
       COALESCE(n.account_group, '') AS account_group
FROM {{ ref('dim_account_key') }} k
LEFT JOIN {{ ref('seed_account_name') }} n ON n.account_key = k.account_key
