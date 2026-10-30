-- One row per ad request, filled or not. Used for fill rate, latency and
-- traffic balance between members.
CREATE TABLE IF NOT EXISTS ad_requests
(
    event_date          Date DEFAULT toDate(received_at),
    received_at         DateTime64(3, 'UTC'),
    request_id          UUID,
    test                Bool DEFAULT false,

    publisher_member_id UUID,
    app_id              UUID,
    placement_id        UUID,
    format              LowCardinality(String),

    platform            LowCardinality(String),
    country             LowCardinality(String),
    language            LowCardinality(String),
    sdk_version         LowCardinality(String),
    installation_id     UUID,

    filled              Bool,
    no_fill_reason      LowCardinality(String),
    ad_id               UUID,  -- zero UUID when not filled
    campaign_id         UUID,
    creative_id         UUID,
    latency_us          UInt32
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_date, app_id, placement_id, received_at)
TTL event_date + INTERVAL 3 MONTH
