#!/usr/bin/env bash
# End-to-end check of a running stack seeded with deploy/seed/dev.sql:
# serve an ad, report events, verify caps, forgery rejection and storage.
# Usage: deploy/test/smoke.sh [base_url]   (run from the deploy/ directory)
set -euo pipefail

BASE=${1:-http://localhost:8080}
PLACEMENT=00000000-0000-4000-8000-000000000021
APP=00000000-0000-4000-8000-000000000011
RACER_CREATIVE=00000000-0000-4000-8000-000000000041
uuid() { cat /proc/sys/kernel/random/uuid 2>/dev/null || uuidgen | tr 'A-Z' 'a-z'; }
INSTALL=$(uuid)

fail() { echo "FAIL: $*" >&2; exit 1; }
ch() { docker compose exec -T clickhouse clickhouse-client --user openad --password "$CLICKHOUSE_PASSWORD" -d openad -q "$1"; }

ad_request() {
  cat <<JSON
{"id":"$(uuid)","placement_id":"$PLACEMENT",
 "app":{"id":"$APP","bundle":"com.example.puzzle","version":"1.0"},
 "device":{"platform":"android","language":"ru"},
 "user":{"installation_id":"$INSTALL"},
 "sdk":{"name":"openad-android","version":"0.1.0"}}
JSON
}

echo "1. Ad request is filled with the other member's campaign"
AD=$(curl -fsS -X POST "$BASE/v1/ad" -H 'Content-Type: application/json' -d "$(ad_request)")
[ "$(jq -r .ad.creative.id <<<"$AD")" = "$RACER_CREATIVE" ] || fail "unexpected ad: $AD"
[ "$(jq -r .ad.disclosure.label <<<"$AD")" = "Реклама" ] || fail "missing disclosure: $AD"
TOKEN=$(jq -r .ad.event_token <<<"$AD")
AD_ID=$(jq -r .ad.id <<<"$AD")

echo "2. Events are accepted once, duplicates detected"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
RES=$(curl -fsS -X POST "$BASE/v1/events" -H 'Content-Type: application/json' -d "{\"events\":[
  {\"type\":\"loaded\",\"event_token\":\"$TOKEN\",\"ts\":\"$NOW\"},
  {\"type\":\"impression\",\"event_token\":\"$TOKEN\",\"ts\":\"$NOW\"},
  {\"type\":\"impression\",\"event_token\":\"$TOKEN\",\"ts\":\"$NOW\"},
  {\"type\":\"click\",\"event_token\":\"$TOKEN\",\"ts\":\"$NOW\"}]}")
[ "$(jq -c '[.results[].status]' <<<"$RES")" = '["accepted","accepted","duplicate","accepted"]' ] || fail "events: $RES"

echo "3. Tampered token is rejected"
# Flip the first character of the MAC (the last one may only carry padding bits).
MAC=${TOKEN##*.}
case ${MAC:0:1} in A) FLIP=B ;; *) FLIP=A ;; esac
BAD="${TOKEN%.*}.${FLIP}${MAC:1}"
RES=$(curl -fsS -X POST "$BASE/v1/events" -H 'Content-Type: application/json' \
  -d "{\"events\":[{\"type\":\"close\",\"event_token\":\"$BAD\",\"ts\":\"$NOW\"}]}")
[ "$(jq -r '.results[0].reason' <<<"$RES")" = "invalid_token" ] || fail "forged token: $RES"

echo "4. Frequency cap: second ad within a minute is not filled"
HDR=$(curl -sS -o /dev/null -D - -X POST "$BASE/v1/ad" -H 'Content-Type: application/json' -d "$(ad_request)")
grep -q '^HTTP/1.1 204' <<<"$HDR" || fail "expected 204: $HDR"
grep -qi '^x-openad-no-fill-reason: frequency_cap' <<<"$HDR" || fail "expected frequency_cap: $HDR"

echo "5. Rows reach ClickHouse"
for i in $(seq 1 15); do
  N=$(ch "SELECT count() FROM events WHERE ad_id = '$AD_ID'")
  [ "$N" = 3 ] && break
  sleep 1
done
[ "$N" = 3 ] || fail "expected 3 events in ClickHouse, got $N"
R=$(ch "SELECT countIf(filled), countIf(no_fill_reason = 'frequency_cap') FROM ad_requests WHERE installation_id = '$INSTALL'")
[ "$R" = "$(printf '1\t1')" ] || fail "ad_requests: $R"
S=$(ch "SELECT sum(count) FROM daily_stats WHERE campaign_id = '00000000-0000-4000-8000-000000000031' AND event_type = 'impression'")
[ "$S" -ge 1 ] || fail "daily_stats not populated"

echo "OK"
