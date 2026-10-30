package selector

import (
	"math"
	"testing"
	"time"

	"github.com/Shtrihan/OpenAd/server/internal/catalog"
)

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func set(vs ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range vs {
		m[v] = true
	}
	return m
}

func image(id string) *catalog.Creative {
	return &catalog.Creative{ID: id, Type: "image", Renditions: []catalog.Rendition{{URL: "u"}}}
}

func campaign(id, member string, weight int) *catalog.Campaign {
	return &catalog.Campaign{ID: id, MemberID: member, AppID: "app-" + id, Weight: weight,
		Creatives: []*catalog.Creative{image("cr-" + id)}}
}

func req() Request {
	return Request{
		Placement: &catalog.Placement{ID: "p", MemberID: "pub", Format: "interstitial", AllowVideo: true},
		Platform:  "android", Country: "RU", Language: "ru", Video: true, Now: now,
	}
}

func ids(cs []*catalog.Campaign) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestEligibleTargeting(t *testing.T) {
	own := campaign("own", "pub", 100)

	wrongFormat := campaign("fmt", "a", 100)
	wrongFormat.Formats = set("rewarded")

	rightFormat := campaign("fmt-ok", "a", 100)
	rightFormat.Formats = set("interstitial", "popup")

	wrongPlatform := campaign("plat", "a", 100)
	wrongPlatform.Platforms = set("ios")

	wrongCountry := campaign("cc", "a", 100)
	wrongCountry.Countries = set("DE")

	wrongLang := campaign("lang", "a", 100)
	wrongLang.Languages = set("en")

	notStarted := campaign("future", "a", 100)
	notStarted.StartsAt = now.Add(time.Hour)

	ended := campaign("ended", "a", 100)
	ended.EndsAt = now // end is exclusive

	running := campaign("running", "a", 100)
	running.StartsAt, running.EndsAt = now.Add(-time.Hour), now.Add(time.Hour)

	blockedApp := campaign("blocked", "a", 100)

	videoTooLong := &catalog.Campaign{ID: "long-video", MemberID: "a", Weight: 100,
		Creatives: []*catalog.Creative{{ID: "v", Type: "video", DurationMs: 60_000}}}

	plain := campaign("plain", "b", 100)

	snap := &catalog.Snapshot{Campaigns: []*catalog.Campaign{
		own, wrongFormat, rightFormat, wrongPlatform, wrongCountry, wrongLang,
		notStarted, ended, running, blockedApp, videoTooLong, plain,
	}}
	snap.Block("app", blockedApp.AppID)

	r := req()
	r.Placement.MaxVideoDurationS = 30
	got := ids(Eligible(snap, r))
	want := []string{"fmt-ok", "running", "plain"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestUnknownCountryOnlyMatchesUntargeted(t *testing.T) {
	targeted := campaign("t", "a", 1)
	targeted.Countries = set("RU")
	open := campaign("o", "a", 1)
	snap := &catalog.Snapshot{Campaigns: []*catalog.Campaign{targeted, open}}

	r := req()
	r.Country = ""
	if got := ids(Eligible(snap, r)); len(got) != 1 || got[0] != "o" {
		t.Fatalf("got %v", got)
	}
}

func TestVideoRespectsClientAndPlacement(t *testing.T) {
	c := &catalog.Campaign{ID: "v", MemberID: "a", Weight: 1,
		Creatives: []*catalog.Creative{{ID: "v1", Type: "video", DurationMs: 15_000}}}
	snap := &catalog.Snapshot{Campaigns: []*catalog.Campaign{c}}

	r := req()
	if len(Eligible(snap, r)) != 1 {
		t.Fatal("video should be eligible")
	}
	r.Video = false
	if len(Eligible(snap, r)) != 0 {
		t.Fatal("client without video support")
	}
	r = req()
	r.Placement.AllowVideo = false
	if len(Eligible(snap, r)) != 0 {
		t.Fatal("placement without video")
	}
}

func TestPickIsWeighted(t *testing.T) {
	a, b := campaign("a", "x", 300), campaign("b", "y", 100)
	counts := map[string]int{}
	const n = 40_000
	for i := 0; i < n; i++ {
		c, cr := Pick([]*catalog.Campaign{a, b}, req(), nil, nil)
		if cr == nil {
			t.Fatal("no creative")
		}
		counts[c.ID]++
	}
	share := float64(counts["a"]) / n
	if math.Abs(share-0.75) > 0.02 {
		t.Fatalf("share of a = %.3f, want ≈0.75", share)
	}
}

func TestPickSkipsExcluded(t *testing.T) {
	a, b := campaign("a", "x", 1000), campaign("b", "y", 1)
	skipA := func(c *catalog.Campaign) bool { return c.ID == "a" }
	for i := 0; i < 100; i++ {
		if c, _ := Pick([]*catalog.Campaign{a, b}, req(), skipA, nil); c.ID != "b" {
			t.Fatalf("picked %s", c.ID)
		}
	}
	all := func(*catalog.Campaign) bool { return true }
	if c, cr := Pick([]*catalog.Campaign{a, b}, req(), all, nil); c != nil || cr != nil {
		t.Fatal("expected no fill")
	}
}

func BenchmarkEligiblePick(b *testing.B) {
	snap := &catalog.Snapshot{}
	for i := 0; i < 500; i++ {
		c := campaign(string(rune('a'+i%26))+time.Duration(i).String(), "m", 100)
		c.Countries = set("RU", "BY", "KZ")
		c.Platforms = set("android", "ios")
		snap.Campaigns = append(snap.Campaigns, c)
	}
	r := req()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Pick(Eligible(snap, r), r, nil, nil)
	}
}
