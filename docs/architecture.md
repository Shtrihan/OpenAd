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
- **Request path.** `POST /v1/ad`: validate → look up placement in the
  snapshot → blocklist → targeting (format, platform, country, language,
  schedule, demand mode) → one Redis `MGET` for frequency
  caps → weighted random pick → sign token. If Redis is down the server
  answers 503 rather than serve without caps.
- **Portfolio first.** An app never promotes itself. A placement's `demand`
  is `portfolio` by default — only the owner's other apps — or
  `portfolio_and_circle`, which also admits campaigns of invited members.
- **Caps count impressions, not responses.** An ad that was fetched but never
  shown does not use up the user's cap. Caps are per installation: placement
  minimum interval, per session, per UTC day; campaign per UTC day.
- **Exactly-once counting.** The ad server deduplicates by `(ad_id, type)` in
  Redis before inserting into ClickHouse. The `events` table is a
  `ReplacingMergeTree` on the same key as a safety net for ad-hoc queries
  (`FINAL`); dashboard reports read `daily_stats`. Events like `click` or
  `reward` are rejected as `out_of_order` unless the ad's impression was
  recorded first. Rows are buffered in memory and written in batches; when the
  buffer is full the event endpoint answers 503 before touching Redis, so
  nothing is marked as seen without being stored.
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
