-- Pre-aggregated daily counters for dashboard reports.
-- Read with sum(count) ... GROUP BY, since rows are merged lazily.
CREATE TABLE IF NOT EXISTS daily_stats
(
    event_date           Date,
    test                 Bool,
    publisher_member_id  UUID,
    app_id               UUID,
    placement_id         UUID,
    format               LowCardinality(String),
    advertiser_member_id UUID,
    campaign_id          UUID,
    creative_id          UUID,
    platform             LowCardinality(String),
    country              LowCardinality(String),
    event_type           LowCardinality(String),
    count                UInt64,
    visible_ms           UInt64,
    played_ms            UInt64
)
ENGINE = SummingMergeTree((count, visible_ms, played_ms))
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_date, test, publisher_member_id, app_id, placement_id, format,
          advertiser_member_id, campaign_id, creative_id, platform, country, event_type)
TTL event_date + INTERVAL 25 MONTH
