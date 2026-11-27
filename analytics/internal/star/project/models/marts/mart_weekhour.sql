{{ config(materialized='table') }}
SELECT '{{ var('tz') }}' AS tz, iso_weekday, local_hour,
       count(*) FILTER (WHERE actor = 'human') AS human_events,
       count(*) FILTER (WHERE actor = 'agent') AS agent_events
FROM {{ ref('int_event_local') }}
WHERE actor IN ('human', 'agent')
GROUP BY iso_weekday, local_hour
