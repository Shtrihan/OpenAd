# Architecture

```
 SDKs (web, Android, iOS, wrappers)
   │  POST /v1/ad, POST /v1/events, GET /v1/t (VAST pixels)
   ▼
 adserver (Go) ──────────────► app owner's server (reward callback, HMAC-signed)
   │       │        │
   │       │        └─ Redis: frequency caps, event dedup
   │       └─ ClickHouse: ad_requests, events → daily_stats (materialized view)
   └─ PostgreSQL: members, apps, placements, campaigns, creatives
        ▲
 dashboard ── uploads ──► video worker (ffmpeg) ──► object storage + CDN
```

## Principles

- **One server, thin clients.** All selection logic is on the server. SDKs
  render, measure visibility and report events — so rules can change without
  app updates.
- **Reference data in PostgreSQL, volume data in ClickHouse.** The ad server
  never queries PostgreSQL on the request path: it keeps the whole active
  catalog in memory and reloads it on `NOTIFY openad_catalog`, which triggers
  on every catalog table emit.
- **Every event is signed.** `POST /v1/ad` returns an opaque `event_token`:
  an HMAC over the ad id, placement, campaign, creative and expiry, signed with
  a key from `signing_keys` (several keys may be valid during rotation). Events
  without a valid token are rejected, so counts cannot be forged by replaying
  ids.
- **Exactly-once counting.** The ad server deduplicates by `(ad_id, type)` in
  Redis before inserting into ClickHouse. The `events` table is a
  `ReplacingMergeTree` on the same key as a safety net for ad-hoc queries
  (`FINAL`); dashboard reports read `daily_stats`.
- **Rewards are server-side.** The client `reward` event alone never grants
  anything; after checks the server calls the app owner's endpoint, which
  grants the reward idempotently by `reward_id`.
- **User protection is data, not code.** Frequency caps and close delay are
  per-placement columns; the close delay is capped at 5 s by a DB constraint.

## Privacy

The SDK sends a random per-installation id, never an advertising id. Raw IPs
are used only for country lookup and stored as a keyed hash.

## Russian ad labelling

Creatives carry `advertiser_label` and `erid`; when present, the API returns
them in `disclosure` and SDKs must display them. Registering creatives with an
ОРД operator is a separate integration, not yet built.
