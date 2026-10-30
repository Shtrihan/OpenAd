CREATE MATERIALIZED VIEW IF NOT EXISTS daily_stats_mv TO daily_stats AS
SELECT
    event_date,
    test,
    publisher_member_id,
    app_id,
    placement_id,
    format,
    advertiser_member_id,
    campaign_id,
    creative_id,
    platform,
    country,
    event_type,
    count()                 AS count,
    sum(toUInt64(visible_ms)) AS visible_ms,
    sum(toUInt64(played_ms))  AS played_ms
FROM events
GROUP BY
    event_date, test, publisher_member_id, app_id, placement_id, format,
    advertiser_member_id, campaign_id, creative_id, platform, country, event_type
