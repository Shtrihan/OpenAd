# OpenAd

Self-hosted ad network for app cross-promotion within a trusted group of developers.

- **Formats:** interstitial, popup, rewarded — static banners and video (VAST 4)
- **Platforms:** Web, Android, iOS, desktop (Electron/Tauri); wrappers for Flutter, React Native, Unity
- **Honest metrics:** an impression counts only when the ad is actually visible; signed, deduplicated events
- **User-friendly:** ads only at natural pauses, a real close button, frequency caps
- **Stack:** Go, PostgreSQL, ClickHouse, Redis, ffmpeg; deploys with a single `docker compose up`

> Status: early development — stage 2 (ad server core). The server selects and
> serves image ads, tracks signed events and enforces frequency caps; video
> (VAST) arrives in stage 3.

## Quick start

Requires Docker with Compose v2.

```bash
cp deploy/.env.example deploy/.env   # set the passwords and IP_HASH_KEY
make up
curl localhost:8080/readyz
make seed                            # optional: two demo members and a campaign
```

`make up` starts PostgreSQL, ClickHouse and Redis, applies migrations, then
starts the ad server. Data lives in Docker volumes and survives `make down`.

## Repository layout

| Path | What |
|---|---|
| [`api/openapi.yaml`](api/openapi.yaml) | Contract of the public API: ad requests, events, reward callback |
| [`server/`](server) | Ad server (Go) |
| [`deploy/`](deploy) | Docker Compose stack and database migrations |
| [`docs/`](docs) | Architecture and design notes |
| `dashboard/` | Member dashboard — stage 5 |
| `sdk/` | Web, Android, iOS SDKs and wrappers — stages 4, 6, 8 |

## Development

```bash
make test       # Go unit tests (needs Go 1.23+)
make lint-api   # validate the OpenAPI contract (needs Node)
make smoke      # end-to-end test against the running, seeded stack
make loadtest   # latency / throughput check against the running stack
```

CI runs unit tests, the API lint, and against a full `docker compose up`:
migrations, the end-to-end smoke test and a load test (p95 < 30 ms).

### Geolocation

Country targeting needs the client country. Either put the server behind a
CDN that sets a country header (`OPENAD_GEO_COUNTRY_HEADER=CF-IPCountry` for
Cloudflare), or mount a MaxMind-format country database (GeoLite2 Country or
DB-IP Lite) and set `OPENAD_GEOIP_DB`. Without either, only campaigns without
country targeting are served.

## Roadmap

1. ~~Foundation~~ — repo, schemas, API contract, Docker Compose
2. **Ad server core** — campaign cache, targeting, frequency caps, rotation, signed events ← *now*
3. Video pipeline — ffmpeg transcoding, CDN, VAST 4
4. Web SDK
5. Member dashboard
6. Android and iOS SDKs
7. Rewards and anti-fraud
8. Flutter / React Native / Unity wrappers and pilot

## License

[Apache 2.0](LICENSE)
