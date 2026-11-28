SELECT tz, slot_key, local_date, local_hour, iso_weekday FROM {{ ref('bridge_time_slot') }}
