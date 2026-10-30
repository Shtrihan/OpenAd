DROP TABLE IF EXISTS blocklist, signing_keys, creative_renditions, creatives, campaigns,
    placements, apps, invites, users, members CASCADE;
DROP FUNCTION IF EXISTS notify_catalog_changed();
DROP FUNCTION IF EXISTS set_updated_at();
DROP TYPE IF EXISTS creative_status, creative_type, entity_status, ad_format, platform,
    user_role, member_status;
