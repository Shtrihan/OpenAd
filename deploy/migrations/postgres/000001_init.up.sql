-- Reference data for OpenAd: members, apps, placements, campaigns, creatives.
-- High-volume data (requests, events) lives in ClickHouse, not here.

CREATE TYPE member_status    AS ENUM ('active', 'suspended');
CREATE TYPE user_role        AS ENUM ('owner', 'admin', 'editor', 'viewer');
CREATE TYPE platform         AS ENUM ('web', 'android', 'ios', 'desktop');
CREATE TYPE ad_format        AS ENUM ('interstitial', 'popup', 'rewarded');
CREATE TYPE entity_status    AS ENUM ('active', 'paused', 'archived');
CREATE TYPE creative_type    AS ENUM ('image', 'video');
CREATE TYPE creative_status  AS ENUM ('uploaded', 'processing', 'ready', 'failed', 'archived');

-- Reusable trigger that keeps updated_at current.
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- A member is one developer or team in the closed circle.
-- Their own campaigns are never shown in their own apps.
CREATE TABLE members (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    status      member_status NOT NULL DEFAULT 'active',
    is_operator boolean NOT NULL DEFAULT false, -- network operator (can invite, sees everything)
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Dashboard accounts. A member may have several people with different roles.
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id     uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    email         text NOT NULL CHECK (email = lower(email) AND position('@' IN email) > 1),
    password_hash text NOT NULL,
    role          user_role NOT NULL DEFAULT 'owner',
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (email);
CREATE INDEX users_member_id_idx ON users (member_id);

-- Invitations: either to a new member (member_id NULL) or into an existing one.
-- Only a hash of the invite code is stored.
CREATE TABLE invites (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash   bytea NOT NULL UNIQUE,
    email       text NOT NULL CHECK (email = lower(email)),
    member_id   uuid REFERENCES members (id) ON DELETE CASCADE,
    role        user_role NOT NULL DEFAULT 'owner',
    created_by  uuid NOT NULL REFERENCES users (id),
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- An app or site of a member. It both shows ads (via placements)
-- and can be promoted (via campaigns).
CREATE TABLE apps (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id           uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    name                text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    platform            platform NOT NULL,
    -- Bundle id, package name, or site domain. Used to validate ad requests.
    bundle              text NOT NULL CHECK (length(bundle) BETWEEN 1 AND 255),
    store_url           text,
    icon_url            text,
    status              entity_status NOT NULL DEFAULT 'active',
    -- Server-to-server reward callback; secret signs the webhook (HMAC-SHA256).
    reward_callback_url text,
    reward_secret       text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT apps_reward_secret_chk CHECK (reward_callback_url IS NULL OR reward_secret IS NOT NULL)
);
CREATE UNIQUE INDEX apps_platform_bundle_key ON apps (platform, bundle) WHERE status <> 'archived';
CREATE INDEX apps_member_id_idx ON apps (member_id);

-- A place inside an app where ads are shown, with its own user-protection limits.
CREATE TABLE placements (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id              uuid NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    name                text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    format              ad_format NOT NULL,
    status              entity_status NOT NULL DEFAULT 'active',
    -- Frequency caps per installation; NULL means no cap.
    min_interval_s      integer CHECK (min_interval_s >= 0),
    max_per_session     integer CHECK (max_per_session > 0),
    max_per_day         integer CHECK (max_per_day > 0),
    -- Close button delay; capped at 5 s to stay within store and Better Ads rules.
    close_delay_ms      integer NOT NULL DEFAULT 0 CHECK (close_delay_ms BETWEEN 0 AND 5000),
    allow_video         boolean NOT NULL DEFAULT true,
    max_video_duration_s integer CHECK (max_video_duration_s > 0),
    -- Rewarded only.
    reward_name         text,
    reward_amount       integer CHECK (reward_amount > 0),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT placements_reward_chk CHECK (
        (format = 'rewarded') = (reward_name IS NOT NULL AND reward_amount IS NOT NULL)
    )
);
CREATE INDEX placements_app_id_idx ON placements (app_id);

-- A campaign promotes one app of a member. Empty targeting arrays mean "any".
CREATE TABLE campaigns (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id       uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    app_id          uuid NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    status          entity_status NOT NULL DEFAULT 'paused',
    -- Relative share in rotation among eligible campaigns.
    weight          integer NOT NULL DEFAULT 100 CHECK (weight BETWEEN 1 AND 10000),
    formats         ad_format[] NOT NULL DEFAULT '{}',
    platforms       platform[]  NOT NULL DEFAULT '{}',
    countries       char(2)[]   NOT NULL DEFAULT '{}', -- ISO 3166-1 alpha-2, upper case
    languages       text[]      NOT NULL DEFAULT '{}', -- ISO 639-1, lower case
    -- Per-installation cap for this campaign across all placements.
    max_per_user_day integer CHECK (max_per_user_day > 0),
    starts_at       timestamptz,
    ends_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT campaigns_schedule_chk CHECK (starts_at IS NULL OR ends_at IS NULL OR starts_at < ends_at)
);
CREATE INDEX campaigns_member_id_idx ON campaigns (member_id);
CREATE INDEX campaigns_active_idx ON campaigns (status) WHERE status = 'active';

CREATE TABLE creatives (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id      uuid NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    type             creative_type NOT NULL,
    status           creative_status NOT NULL DEFAULT 'uploaded',
    name             text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    click_url        text NOT NULL,
    -- Disclosure shown with the ad; erid is the Russian ad registry (ОРД) token.
    advertiser_label text,
    erid             text,
    -- Original upload in object storage; renditions are derived from it.
    source_key       text NOT NULL,
    duration_ms      integer CHECK (duration_ms > 0), -- video only
    error            text,                             -- set when status = 'failed'
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT creatives_duration_chk CHECK (type = 'video' OR duration_ms IS NULL)
);
CREATE INDEX creatives_campaign_id_idx ON creatives (campaign_id);

-- Concrete files served to devices: image sizes, or MP4 qualities from ffmpeg.
CREATE TABLE creative_renditions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    creative_id uuid NOT NULL REFERENCES creatives (id) ON DELETE CASCADE,
    url         text NOT NULL,
    mime        text NOT NULL,
    width       integer NOT NULL CHECK (width > 0),
    height      integer NOT NULL CHECK (height > 0),
    bitrate_kbps integer CHECK (bitrate_kbps > 0),
    size_bytes  bigint CHECK (size_bytes > 0),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (creative_id, mime, width, height)
);

-- Keys for signing event tokens. Several can be valid at once for rotation.
CREATE TABLE signing_keys (
    id         text PRIMARY KEY,           -- short key id embedded in tokens
    secret     bytea NOT NULL CHECK (length(secret) >= 32),
    active     boolean NOT NULL DEFAULT true, -- used for signing new tokens
    created_at timestamptz NOT NULL DEFAULT now(),
    retired_at timestamptz                  -- stop accepting after this time
);

-- Manual blocks for anti-fraud: installations, IP ranges, apps.
CREATE TABLE blocklist (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       text NOT NULL CHECK (kind IN ('installation', 'ip', 'app')),
    value      text NOT NULL,
    reason     text,
    created_by uuid REFERENCES users (id),
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, value)
);

CREATE TRIGGER members_updated_at    BEFORE UPDATE ON members    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER users_updated_at      BEFORE UPDATE ON users      FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER apps_updated_at       BEFORE UPDATE ON apps       FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER placements_updated_at BEFORE UPDATE ON placements FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER campaigns_updated_at  BEFORE UPDATE ON campaigns  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER creatives_updated_at  BEFORE UPDATE ON creatives  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The ad server keeps an in-memory campaign cache; this notifies it on changes.
CREATE FUNCTION notify_catalog_changed() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('openad_catalog', TG_TABLE_NAME);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER apps_notify       AFTER INSERT OR UPDATE OR DELETE ON apps                FOR EACH STATEMENT EXECUTE FUNCTION notify_catalog_changed();
CREATE TRIGGER placements_notify AFTER INSERT OR UPDATE OR DELETE ON placements          FOR EACH STATEMENT EXECUTE FUNCTION notify_catalog_changed();
CREATE TRIGGER campaigns_notify  AFTER INSERT OR UPDATE OR DELETE ON campaigns           FOR EACH STATEMENT EXECUTE FUNCTION notify_catalog_changed();
CREATE TRIGGER creatives_notify  AFTER INSERT OR UPDATE OR DELETE ON creatives           FOR EACH STATEMENT EXECUTE FUNCTION notify_catalog_changed();
CREATE TRIGGER renditions_notify AFTER INSERT OR UPDATE OR DELETE ON creative_renditions FOR EACH STATEMENT EXECUTE FUNCTION notify_catalog_changed();
CREATE TRIGGER members_notify    AFTER INSERT OR UPDATE OR DELETE ON members             FOR EACH STATEMENT EXECUTE FUNCTION notify_catalog_changed();
CREATE TRIGGER blocklist_notify  AFTER INSERT OR UPDATE OR DELETE ON blocklist           FOR EACH STATEMENT EXECUTE FUNCTION notify_catalog_changed();
