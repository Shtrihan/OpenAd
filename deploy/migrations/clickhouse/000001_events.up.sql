-- One row per accepted ad event. Deduplication by (ad_id, event_type) happens
-- in the ad server via Redis before insert; ReplacingMergeTree is a safety net
-- so that ad-hoc queries with FINAL stay exact even if Redis state was lost.
CREATE TABLE IF NOT EXISTS events
(
    event_date           Date DEFAULT toDate(received_at),
    received_at          DateTime64(3, 'UTC'),
    client_ts            DateTime64(3, 'UTC'),
    event_type           LowCardinality(String),
    ad_id                UUID,
    request_id           UUID,
    test                 Bool DEFAULT false,

    -- Supply side: where the ad was shown.
    publisher_member_id  UUID,
    app_id               UUID,
    placement_id         UUID,
    format               LowCardinality(String),

    -- Demand side: what was shown.
    advertiser_member_id UUID,
    campaign_id          UUID,
    creative_id          UUID,
    creative_type        LowCardinality(String),

    -- Context.
    platform             LowCardinality(String),
    os                   LowCardinality(String),
    country              LowCardinality(String),
    language             LowCardinality(String),
    sdk_name             LowCardinality(String),
    sdk_version          LowCardinality(String),
    app_version          LowCardinality(String),
    installation_id      UUID,
    ip_hash              UInt64,  -- keyed hash, never the raw IP

    -- Event details.
    error_code           UInt16 DEFAULT 0,
    visible_ms           UInt32 DEFAULT 0,
    played_ms            UInt32 DEFAULT 0
)
ENGINE = ReplacingMergeTree(received_at)
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_date, app_id, event_type, ad_id)
TTL event_date + INTERVAL 13 MONTH
SETTINGS index_granularity = 8192
