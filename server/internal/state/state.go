// Package state keeps per-user serving state in Redis: frequency caps and
// event deduplication. Caps count impressions (what the user actually saw),
// not served ads.
package state

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Shtrihan/OpenAd/server/internal/catalog"
	"github.com/Shtrihan/OpenAd/server/internal/token"
)

const (
	dedupTTL   = 48 * time.Hour // longer than any token lifetime
	dayTTL     = 48 * time.Hour // covers time zones and late events
	lastTTL    = 7 * 24 * time.Hour
	sessionTTL = 24 * time.Hour
)

// Result of recording one event.
type Result int

const (
	Accepted Result = iota
	Duplicate
	OutOfOrder // event needs an impression that was not recorded
)

type Store struct {
	rdb redis.UniversalClient
}

func New(rdb redis.UniversalClient) *Store { return &Store{rdb: rdb} }

func day(t time.Time) string { return t.UTC().Format("20060102") }

func placementLastKey(p, inst string) string { return "fc:pl:" + p + ":" + inst + ":last" }
func placementDayKey(p, inst string, t time.Time) string {
	return "fc:pl:" + p + ":" + inst + ":d:" + day(t)
}
func placementSessionKey(p, inst, s string) string { return "fc:pl:" + p + ":" + inst + ":s:" + s }
func campaignDayKey(c, inst string, t time.Time) string {
	return "fc:cp:" + c + ":" + inst + ":d:" + day(t)
}
func eventKey(adID, typ string) string { return "ev:" + adID + ":" + typ }

// Caps is the outcome of a frequency cap check.
type Caps struct {
	PlacementCapped bool
	Campaigns       map[string]bool // campaign id -> capped
}

// CheckCaps reads all counters relevant to one request in a single round trip.
func (s *Store) CheckCaps(ctx context.Context, p *catalog.Placement, inst, session string, cands []*catalog.Campaign, now time.Time) (Caps, error) {
	keys := []string{placementLastKey(p.ID, inst), placementDayKey(p.ID, inst, now), ""}
	if session != "" {
		keys[2] = placementSessionKey(p.ID, inst, session)
	} else {
		keys = keys[:2]
	}
	var capped []*catalog.Campaign
	for _, c := range cands {
		if c.MaxPerUserDay > 0 {
			capped = append(capped, c)
			keys = append(keys, campaignDayKey(c.ID, inst, now))
		}
	}
	vals, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return Caps{}, err
	}
	num := func(i int) int64 {
		if i >= len(vals) || vals[i] == nil {
			return 0
		}
		n, _ := strconv.ParseInt(vals[i].(string), 10, 64)
		return n
	}

	var res Caps
	if last := num(0); p.MinIntervalS > 0 && last > 0 && now.Unix()-last < int64(p.MinIntervalS) {
		res.PlacementCapped = true
	}
	if p.MaxPerDay > 0 && num(1) >= int64(p.MaxPerDay) {
		res.PlacementCapped = true
	}
	if session != "" && p.MaxPerSession > 0 && num(2) >= int64(p.MaxPerSession) {
		res.PlacementCapped = true
	}
	base := 3
	if session == "" {
		base = 2
	}
	for i, c := range capped {
		if num(base+i) >= int64(c.MaxPerUserDay) {
			if res.Campaigns == nil {
				res.Campaigns = map[string]bool{}
			}
			res.Campaigns[c.ID] = true
		}
	}
	return res, nil
}

// recordScript atomically checks ordering, deduplicates, and for impressions
// updates frequency counters.
//
//	KEYS[1] dedup key of this event
//	KEYS[2] dedup key of the ad's impression (ordering prerequisite)
//	KEYS[3] placement "last shown" key            (impressions only)
//	KEYS[4..] counters to increment                (impressions only)
//	ARGV: dedup ttl, require impression (0|1), now, last ttl, counter ttl, session ttl, session key index
var recordScript = redis.NewScript(`
if ARGV[2] == '1' and redis.call('EXISTS', KEYS[2]) == 0 then
  return -1
end
if not redis.call('SET', KEYS[1], '1', 'NX', 'EX', ARGV[1]) then
  return 0
end
if #KEYS >= 3 then
  redis.call('SET', KEYS[3], ARGV[3], 'EX', ARGV[4])
  for i = 4, #KEYS do
    redis.call('INCR', KEYS[i])
    local ttl = ARGV[5]
    if tostring(i) == ARGV[7] then ttl = ARGV[6] end
    redis.call('EXPIRE', KEYS[i], ttl)
  end
end
return 1
`)

// needsImpression lists events that only make sense after the ad was shown.
var needsImpression = map[string]bool{
	"viewable": true, "click": true, "close": true, "reward": true,
	"video_start": true, "video_q1": true, "video_mid": true, "video_q3": true, "video_complete": true,
}

// Record deduplicates an event and applies its side effects atomically.
func (s *Store) Record(ctx context.Context, c *token.Claims, typ string, now time.Time) (Result, error) {
	keys := []string{eventKey(c.AdID, typ), eventKey(c.AdID, "impression")}
	sessionIdx := "0"
	if typ == "impression" {
		keys = append(keys,
			placementLastKey(c.PlacementID, c.InstallationID),
			placementDayKey(c.PlacementID, c.InstallationID, now),
			campaignDayKey(c.CampaignID, c.InstallationID, now),
		)
		if c.SessionID != "" {
			keys = append(keys, placementSessionKey(c.PlacementID, c.InstallationID, c.SessionID))
			sessionIdx = strconv.Itoa(len(keys))
		}
	}
	require := "0"
	if needsImpression[typ] {
		require = "1"
	}
	n, err := recordScript.Run(ctx, s.rdb, keys,
		int(dedupTTL.Seconds()), require, now.Unix(),
		int(lastTTL.Seconds()), int(dayTTL.Seconds()), int(sessionTTL.Seconds()), sessionIdx,
	).Int()
	if err != nil {
		return 0, err
	}
	switch n {
	case 1:
		return Accepted, nil
	case 0:
		return Duplicate, nil
	default:
		return OutOfOrder, nil
	}
}

// Ping checks connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.rdb.Ping(ctx).Err() }
