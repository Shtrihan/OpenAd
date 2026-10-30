-- Development / CI fixtures: two members promoting to each other.
-- Idempotent; safe to apply repeatedly.

INSERT INTO members (id, name, is_operator) VALUES
    ('00000000-0000-4000-8000-000000000001', 'Publisher Studio', true),
    ('00000000-0000-4000-8000-000000000002', 'Advertiser Games', false)
ON CONFLICT (id) DO NOTHING;

INSERT INTO apps (id, member_id, name, platform, bundle) VALUES
    ('00000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000001', 'Puzzle', 'android', 'com.example.puzzle'),
    ('00000000-0000-4000-8000-000000000012', '00000000-0000-4000-8000-000000000002', 'Racer',  'android', 'com.example.racer')
ON CONFLICT (id) DO NOTHING;

-- Interstitial in the publisher's app: at most one per minute per installation.
INSERT INTO placements (id, app_id, name, format, min_interval_s, max_per_day, close_delay_ms) VALUES
    ('00000000-0000-4000-8000-000000000021', '00000000-0000-4000-8000-000000000011', 'Level complete', 'interstitial', 60, 20, 3000)
ON CONFLICT (id) DO NOTHING;

INSERT INTO campaigns (id, member_id, app_id, name, status, weight) VALUES
    -- Promotes the advertiser's Racer: eligible in Puzzle.
    ('00000000-0000-4000-8000-000000000031', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000012', 'Racer launch', 'active', 100),
    -- The publisher's own campaign: must never show in their own app.
    ('00000000-0000-4000-8000-000000000032', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000011', 'Puzzle promo', 'active', 10000)
ON CONFLICT (id) DO NOTHING;

INSERT INTO creatives (id, campaign_id, type, status, name, click_url, advertiser_label, source_key) VALUES
    ('00000000-0000-4000-8000-000000000041', '00000000-0000-4000-8000-000000000031', 'image', 'ready', 'Racer portrait',
     'https://play.google.com/store/apps/details?id=com.example.racer', 'Advertiser Games', 'seed/racer.png'),
    ('00000000-0000-4000-8000-000000000042', '00000000-0000-4000-8000-000000000032', 'image', 'ready', 'Puzzle portrait',
     'https://play.google.com/store/apps/details?id=com.example.puzzle', 'Publisher Studio', 'seed/puzzle.png')
ON CONFLICT (id) DO NOTHING;

INSERT INTO creative_renditions (creative_id, url, mime, width, height) VALUES
    ('00000000-0000-4000-8000-000000000041', 'https://cdn.example.com/seed/racer-1080x1920.webp',  'image/webp', 1080, 1920),
    ('00000000-0000-4000-8000-000000000042', 'https://cdn.example.com/seed/puzzle-1080x1920.webp', 'image/webp', 1080, 1920)
ON CONFLICT (creative_id, mime, width, height) DO NOTHING;
