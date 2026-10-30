-- Development / CI fixtures. Idempotent; safe to apply repeatedly.
--
-- Publisher Studio owns two apps (Puzzle, Words) and cross-promotes them.
-- Advertiser Games owns Racer and is part of the invited circle.

INSERT INTO members (id, name, is_operator) VALUES
    ('00000000-0000-4000-8000-000000000001', 'Publisher Studio', true),
    ('00000000-0000-4000-8000-000000000002', 'Advertiser Games', false)
ON CONFLICT (id) DO NOTHING;

INSERT INTO apps (id, member_id, name, platform, bundle) VALUES
    ('00000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000001', 'Puzzle', 'android', 'com.example.puzzle'),
    ('00000000-0000-4000-8000-000000000013', '00000000-0000-4000-8000-000000000001', 'Words',  'android', 'com.example.words'),
    ('00000000-0000-4000-8000-000000000012', '00000000-0000-4000-8000-000000000002', 'Racer',  'android', 'com.example.racer')
ON CONFLICT (id) DO NOTHING;

INSERT INTO placements (id, app_id, name, format, demand, min_interval_s, max_per_day, close_delay_ms) VALUES
    -- Portfolio only (default): shows Words, never Puzzle itself, never Racer.
    ('00000000-0000-4000-8000-000000000021', '00000000-0000-4000-8000-000000000011', 'Level complete', 'interstitial', 'portfolio', 60, 20, 3000),
    -- Opted into the circle: Words or Racer.
    ('00000000-0000-4000-8000-000000000022', '00000000-0000-4000-8000-000000000011', 'Main menu', 'interstitial', 'portfolio_and_circle', NULL, NULL, 3000)
ON CONFLICT (id) DO NOTHING;

INSERT INTO campaigns (id, member_id, app_id, name, status, weight) VALUES
    ('00000000-0000-4000-8000-000000000033', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000013', 'Words promo',  'active', 100),
    -- Promotes Puzzle: must never show inside Puzzle.
    ('00000000-0000-4000-8000-000000000032', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000011', 'Puzzle promo', 'active', 10000),
    ('00000000-0000-4000-8000-000000000031', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000012', 'Racer launch', 'active', 100)
ON CONFLICT (id) DO NOTHING;

INSERT INTO creatives (id, campaign_id, type, status, name, click_url, advertiser_label, source_key) VALUES
    ('00000000-0000-4000-8000-000000000043', '00000000-0000-4000-8000-000000000033', 'image', 'ready', 'Words portrait',
     'https://play.google.com/store/apps/details?id=com.example.words', 'Publisher Studio', 'seed/words.png'),
    ('00000000-0000-4000-8000-000000000042', '00000000-0000-4000-8000-000000000032', 'image', 'ready', 'Puzzle portrait',
     'https://play.google.com/store/apps/details?id=com.example.puzzle', 'Publisher Studio', 'seed/puzzle.png'),
    ('00000000-0000-4000-8000-000000000041', '00000000-0000-4000-8000-000000000031', 'image', 'ready', 'Racer portrait',
     'https://play.google.com/store/apps/details?id=com.example.racer', 'Advertiser Games', 'seed/racer.png')
ON CONFLICT (id) DO NOTHING;

INSERT INTO creative_renditions (creative_id, url, mime, width, height) VALUES
    ('00000000-0000-4000-8000-000000000043', 'https://cdn.example.com/seed/words-1080x1920.webp',  'image/webp', 1080, 1920),
    ('00000000-0000-4000-8000-000000000042', 'https://cdn.example.com/seed/puzzle-1080x1920.webp', 'image/webp', 1080, 1920),
    ('00000000-0000-4000-8000-000000000041', 'https://cdn.example.com/seed/racer-1080x1920.webp',  'image/webp', 1080, 1920)
ON CONFLICT (creative_id, mime, width, height) DO NOTHING;
