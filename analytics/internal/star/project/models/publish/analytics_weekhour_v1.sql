SELECT iso_weekday, local_hour, human_events, agent_events
FROM {{ ref('mart_weekhour') }}
