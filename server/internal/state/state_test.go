package state

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/Shtrihan/OpenAd/server/internal/catalog"
	"github.com/Shtrihan/OpenAd/server/internal/token"
)

func newStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return New(redis.NewClient(&redis.Options{Addr: mr.Addr()})), mr
}

var (
	ctx = context.Background()
	t0  = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
)

func claims(ad string) *token.Claims {
	return &token.Claims{AdID: ad, PlacementID: "p", CampaignID: "c", InstallationID: "i", SessionID: "s"}
}

func TestRecordDedupAndOrder(t *testing.T) {
	s, _ := newStore(t)
	c := claims("ad1")

	steps := []struct {
		typ  string
		want Result
	}{
		{"click", OutOfOrder}, // no impression yet
		{"loaded", Accepted},  // allowed before impression
		{"impression", Accepted},
		{"impression", Duplicate},
		{"click", Accepted},
		{"click", Duplicate},
	}
	for _, st := range steps {
		got, err := s.Record(ctx, c, st.typ, t0)
		if err != nil {
			t.Fatal(err)
		}
		if got != st.want {
			t.Fatalf("%s: got %v, want %v", st.typ, got, st.want)
		}
	}
}

func TestCaps(t *testing.T) {
	s, _ := newStore(t)
	p := &catalog.Placement{ID: "p", MinIntervalS: 60, MaxPerDay: 3, MaxPerSession: 2}
	camp := &catalog.Campaign{ID: "c", MaxPerUserDay: 1}
	other := &catalog.Campaign{ID: "c2"}
	cands := []*catalog.Campaign{camp, other}

	caps, err := s.CheckCaps(ctx, p, "i", "s", cands, t0)
	if err != nil || caps.PlacementCapped || caps.Campaigns["c"] {
		t.Fatalf("fresh user: %+v %v", caps, err)
	}

	if _, err := s.Record(ctx, claims("ad1"), "impression", t0); err != nil {
		t.Fatal(err)
	}

	// Within min interval.
	caps, _ = s.CheckCaps(ctx, p, "i", "s", cands, t0.Add(30*time.Second))
	if !caps.PlacementCapped {
		t.Fatal("min interval not enforced")
	}
	// After interval: placement ok, campaign c hit its daily cap, c2 untouched.
	caps, _ = s.CheckCaps(ctx, p, "i", "s", cands, t0.Add(61*time.Second))
	if caps.PlacementCapped || !caps.Campaigns["c"] || caps.Campaigns["c2"] {
		t.Fatalf("after interval: %+v", caps)
	}

	// Second impression in the session reaches max_per_session.
	if _, err := s.Record(ctx, claims("ad2"), "impression", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	caps, _ = s.CheckCaps(ctx, p, "i", "s", cands, t0.Add(5*time.Minute))
	if !caps.PlacementCapped {
		t.Fatal("session cap not enforced")
	}
	// A new session is allowed again until the daily cap.
	caps, _ = s.CheckCaps(ctx, p, "i", "s-new", cands, t0.Add(5*time.Minute))
	if caps.PlacementCapped {
		t.Fatal("new session should not be capped")
	}
	// Next UTC day resets daily counters.
	caps, _ = s.CheckCaps(ctx, p, "i", "s-new", cands, t0.Add(24*time.Hour))
	if caps.PlacementCapped || caps.Campaigns["c"] {
		t.Fatalf("next day: %+v", caps)
	}
	// Another installation is independent.
	caps, _ = s.CheckCaps(ctx, p, "other", "s", cands, t0.Add(time.Second))
	if caps.PlacementCapped || caps.Campaigns["c"] {
		t.Fatalf("other installation: %+v", caps)
	}
}

func TestCountersExpire(t *testing.T) {
	s, mr := newStore(t)
	if _, err := s.Record(ctx, claims("ad1"), "impression", t0); err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(placementSessionKey("p", "i", "s")); ttl != sessionTTL {
		t.Fatalf("session ttl = %v", ttl)
	}
	if ttl := mr.TTL(placementDayKey("p", "i", t0)); ttl != dayTTL {
		t.Fatalf("day ttl = %v", ttl)
	}
	if ttl := mr.TTL(eventKey("ad1", "impression")); ttl != dedupTTL {
		t.Fatalf("dedup ttl = %v", ttl)
	}
}
