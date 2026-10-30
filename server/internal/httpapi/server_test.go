package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/Shtrihan/OpenAd/server/internal/catalog"
	"github.com/Shtrihan/OpenAd/server/internal/chwriter"
	"github.com/Shtrihan/OpenAd/server/internal/geo"
	"github.com/Shtrihan/OpenAd/server/internal/state"
	"github.com/Shtrihan/OpenAd/server/internal/token"
)

const (
	pubMember  = "11111111-1111-4111-8111-111111111111"
	advMember  = "22222222-2222-4222-8222-222222222222"
	pubApp     = "33333333-3333-4333-8333-333333333333"
	placementI = "44444444-4444-4444-8444-444444444444"
	campaignA  = "55555555-5555-4555-8555-555555555555"
	campaignWn = "66666666-6666-4666-8666-666666666666" // the publisher's own campaign
	install    = "77777777-7777-4777-8777-777777777777"
)

type fixedCatalog struct{ snap *catalog.Snapshot }

func (f fixedCatalog) Snapshot() *catalog.Snapshot { return f.snap }

type memSink[T any] struct {
	mu   sync.Mutex
	rows []T
	cap  int
}

func (m *memSink[T]) Add(rows ...T) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cap > 0 && len(m.rows)+len(rows) > m.cap {
		return false
	}
	m.rows = append(m.rows, rows...)
	return true
}

func (m *memSink[T]) Free() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cap == 0 {
		return 1 << 30
	}
	return m.cap - len(m.rows)
}

func (m *memSink[T]) all() []T {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]T(nil), m.rows...)
}

func testSnapshot() *catalog.Snapshot {
	img := func(id string) *catalog.Creative {
		return &catalog.Creative{ID: id, Type: "image", ClickURL: "https://example.com/app",
			AdvertiserLabel: "Example Ltd", Erid: "2VtzqvXXXX",
			Renditions: []catalog.Rendition{{URL: "https://cdn.example.com/" + id + ".webp", Mime: "image/webp", Width: 1080, Height: 1920}}}
	}
	return &catalog.Snapshot{
		Placements: map[string]*catalog.Placement{placementI: {
			ID: placementI, AppID: pubApp, MemberID: pubMember, Platform: "android", Format: "interstitial",
			MinIntervalS: 60, CloseDelayMs: 3000, AllowVideo: true,
		}},
		Campaigns: []*catalog.Campaign{
			{ID: campaignWn, MemberID: pubMember, AppID: pubApp, Weight: 10000, Creatives: []*catalog.Creative{img("88888888-8888-4888-8888-888888888888")}},
			{ID: campaignA, MemberID: advMember, AppID: "99999999-9999-4999-8999-999999999999", Weight: 1, Creatives: []*catalog.Creative{img("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")}},
		},
		Keyring: token.NewKeyring(token.Key{ID: "k1", Secret: bytes.Repeat([]byte{7}, 32)}),
	}
}

type env struct {
	srv      *httptest.Server
	snap     *catalog.Snapshot
	events   *memSink[chwriter.Event]
	requests *memSink[chwriter.Request]
	mr       *miniredis.Miniredis
	now      time.Time
}

func newEnv(t testing.TB) *env {
	t.Helper()
	mr := miniredis.RunT(t)
	e := &env{
		snap:     testSnapshot(),
		events:   &memSink[chwriter.Event]{},
		requests: &memSink[chwriter.Request]{},
		mr:       mr,
		now:      time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := New("test", map[string]Check{"redis": func(context.Context) error { return nil }}, log, Deps{
		Catalog:  fixedCatalog{e.snap},
		State:    state.New(redis.NewClient(&redis.Options{Addr: mr.Addr()})),
		Events:   e.events,
		Requests: e.requests,
		Geo:      geo.NewResolver([]byte("k")),
		Now:      func() time.Time { return e.now },
	})
	e.srv = httptest.NewServer(api.Handler())
	t.Cleanup(e.srv.Close)
	return e
}

func adReq(overrides ...func(map[string]any)) map[string]any {
	m := map[string]any{
		"id":           "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"placement_id": placementI,
		"app":          map[string]any{"id": pubApp, "bundle": "com.example.pub", "version": "1.0"},
		"device":       map[string]any{"platform": "android", "language": "ru"},
		"user":         map[string]any{"installation_id": install},
		"sdk":          map[string]any{"name": "openad-android", "version": "0.1.0"},
	}
	for _, o := range overrides {
		o(m)
	}
	return m
}

func post(t *testing.T, url string, body any) (*http.Response, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func (e *env) requestAd(t *testing.T, body map[string]any) (*http.Response, adResponse) {
	t.Helper()
	resp, raw := post(t, e.srv.URL+"/v1/ad", body)
	var ad adResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &ad); err != nil {
			t.Fatal(err)
		}
	}
	return resp, ad
}

func (e *env) send(t *testing.T, events ...map[string]any) (int, []eventResult) {
	t.Helper()
	resp, raw := post(t, e.srv.URL+"/v1/events", map[string]any{"events": events})
	var out struct{ Results []eventResult }
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out.Results
}

func ev(typ, tok string) map[string]any {
	return map[string]any{"type": typ, "event_token": tok, "ts": "2026-11-01T12:00:01Z"}
}

func TestHealth(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.srv.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", resp, err)
	}
	resp, err = http.Get(e.srv.URL + "/readyz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("readyz: %v %v", resp, err)
	}
}

func TestReadyzReportsFailures(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := New("test", map[string]Check{"postgres": func(context.Context) error { return errors.New("down") }}, log, Deps{})
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/readyz")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestServeExcludesOwnCampaign(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 20; i++ {
		resp, ad := e.requestAd(t, adReq())
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}
		if ad.Ad.Creative.ID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
			t.Fatalf("served own campaign: %+v", ad.Ad.Creative)
		}
	}
}

func TestServeResponseShape(t *testing.T) {
	e := newEnv(t)
	resp, ad := e.requestAd(t, adReq())
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d", resp.StatusCode)
	}
	a := ad.Ad
	if a.Format != "interstitial" || a.Close.DelayMs != 3000 || a.EventToken == "" ||
		len(a.Creative.Image) != 1 || a.ClickURL == "" || a.Reward != nil {
		t.Fatalf("ad: %+v", a)
	}
	if a.Disclosure.Label != "Реклама" || a.Disclosure.Erid == "" || a.Disclosure.Advertiser == "" {
		t.Fatalf("disclosure: %+v", a.Disclosure)
	}
	if !a.ExpiresAt.Equal(e.now.Add(time.Hour)) {
		t.Fatalf("expires_at %v", a.ExpiresAt)
	}
	reqs := e.requests.all()
	if len(reqs) != 1 || !reqs[0].Filled || reqs[0].AdID.String() != a.ID {
		t.Fatalf("request log: %+v", reqs)
	}
}

func TestServeRejectsBadInput(t *testing.T) {
	e := newEnv(t)
	cases := map[string]func(map[string]any){
		"bad uuid":     func(m map[string]any) { m["id"] = "nope" },
		"bad platform": func(m map[string]any) { m["device"] = map[string]any{"platform": "tv", "language": "ru"} },
		"bad language": func(m map[string]any) { m["device"] = map[string]any{"platform": "android", "language": "RU"} },
		"bad sdk":      func(m map[string]any) { m["sdk"] = map[string]any{"name": "other", "version": "1"} },
	}
	for name, mod := range cases {
		if resp, _ := e.requestAd(t, adReq(mod)); resp.StatusCode != 400 {
			t.Errorf("%s: %d", name, resp.StatusCode)
		}
	}
	wrongApp := func(m map[string]any) { m["app"] = map[string]any{"id": campaignA} }
	if resp, _ := e.requestAd(t, adReq(wrongApp)); resp.StatusCode != 404 {
		t.Errorf("placement of another app: %d", resp.StatusCode)
	}
}

func TestNoFillReasons(t *testing.T) {
	e := newEnv(t)
	e.snap.Block("installation", install)
	resp, _ := e.requestAd(t, adReq())
	if resp.StatusCode != 204 || resp.Header.Get("X-OpenAd-No-Fill-Reason") != "blocked" {
		t.Fatalf("blocked: %d %s", resp.StatusCode, resp.Header.Get("X-OpenAd-No-Fill-Reason"))
	}

	e2 := newEnv(t)
	e2.snap.Campaigns[1].Countries = map[string]bool{"DE": true}
	resp, _ = e2.requestAd(t, adReq())
	if resp.StatusCode != 204 || resp.Header.Get("X-OpenAd-No-Fill-Reason") != "no_eligible_ads" {
		t.Fatalf("targeting: %d", resp.StatusCode)
	}
	if reqs := e2.requests.all(); len(reqs) != 1 || reqs[0].Filled || reqs[0].NoFillReason != "no_eligible_ads" {
		t.Fatalf("no-fill not logged: %+v", reqs)
	}
}

func TestEventFlowAndFrequencyCap(t *testing.T) {
	e := newEnv(t)
	_, ad := e.requestAd(t, adReq())
	tok := ad.Ad.EventToken

	code, res := e.send(t, ev("click", tok), ev("loaded", tok), ev("impression", tok), ev("impression", tok), ev("click", tok))
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	want := []string{"rejected", "accepted", "accepted", "duplicate", "accepted"}
	for i, w := range want {
		if res[i].Status != w {
			t.Fatalf("event %d: got %+v, want %s (all: %+v)", i, res[i], w, res)
		}
	}
	if res[0].Reason != "out_of_order" {
		t.Fatalf("reason: %+v", res[0])
	}
	rows := e.events.all()
	if len(rows) != 3 || rows[1].Type != "impression" || rows[1].CampaignID.String() != campaignA || rows[1].Country != "" {
		t.Fatalf("rows: %+v", rows)
	}

	// The placement has a 60 s minimum interval after an impression.
	e.now = e.now.Add(30 * time.Second)
	if resp, _ := e.requestAd(t, adReq()); resp.StatusCode != 204 || resp.Header.Get("X-OpenAd-No-Fill-Reason") != "frequency_cap" {
		t.Fatalf("cap not applied: %d", resp.StatusCode)
	}
	e.now = e.now.Add(31 * time.Second)
	if resp, _ := e.requestAd(t, adReq()); resp.StatusCode != 200 {
		t.Fatalf("cap not released: %d", resp.StatusCode)
	}
}

func TestEventRejections(t *testing.T) {
	e := newEnv(t)
	_, ad := e.requestAd(t, adReq())
	tok := ad.Ad.EventToken

	forged := token.NewKeyring(token.Key{ID: "k1", Secret: bytes.Repeat([]byte{9}, 32)})
	c, _ := e.snap.Keyring.Verify(tok, e.now)
	c.CampaignID = campaignWn
	forgedTok, _ := forged.Sign(c)

	_, res := e.send(t, ev("impression", forgedTok), ev("teleport", tok), ev("impression", "garbage"))
	for i, reason := range []string{"invalid_token", "unknown_type", "invalid_token"} {
		if res[i].Status != "rejected" || res[i].Reason != reason {
			t.Fatalf("event %d: %+v", i, res[i])
		}
	}

	// Impression after the show window is too late.
	e.now = e.now.Add(61 * time.Minute)
	if _, res := e.send(t, ev("impression", tok)); res[0].Reason != "expired" {
		t.Fatalf("late impression: %+v", res)
	}
	// Any event after token expiry.
	e.now = e.now.Add(7 * time.Hour)
	if _, res := e.send(t, ev("close", tok)); res[0].Reason != "expired" {
		t.Fatalf("expired token: %+v", res)
	}
	if len(e.events.all()) != 0 {
		t.Fatal("rejected events must not be stored")
	}
}

func TestEventBatchLimits(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.send(t); code != 400 {
		t.Fatalf("empty: %d", code)
	}
	many := make([]map[string]any, 51)
	for i := range many {
		many[i] = ev("loaded", "x")
	}
	if code, _ := e.send(t, many...); code != 413 {
		t.Fatalf("too many: %d", code)
	}
}

func TestEventsBackpressure(t *testing.T) {
	e := newEnv(t)
	e.events.cap = 1
	_, ad := e.requestAd(t, adReq())
	if code, _ := e.send(t, ev("loaded", ad.Ad.EventToken), ev("impression", ad.Ad.EventToken)); code != 503 {
		t.Fatalf("expected 503 when buffer is full, got %d", code)
	}
	if e.mr.Exists("ev:" + ad.Ad.ID + ":impression") {
		t.Fatal("refused batch must not mark events as seen")
	}
}

func TestRedisDownFailsClosed(t *testing.T) {
	e := newEnv(t)
	_, ad := e.requestAd(t, adReq())
	e.mr.Close()
	if resp, _ := e.requestAd(t, adReq()); resp.StatusCode != 503 {
		t.Fatalf("ad without caps: %d", resp.StatusCode)
	}
	if code, _ := e.send(t, ev("impression", ad.Ad.EventToken)); code != 503 {
		t.Fatalf("events without dedup: %d", code)
	}
}

func TestPixel(t *testing.T) {
	e := newEnv(t)
	_, ad := e.requestAd(t, adReq())
	for _, q := range []string{"e=impression&tk=" + ad.Ad.EventToken, "e=impression&tk=garbage", "e=bogus"} {
		resp, err := http.Get(e.srv.URL + "/v1/t?" + q)
		if err != nil || resp.StatusCode != 204 {
			t.Fatalf("%s: %v %v", q, resp, err)
		}
	}
	if rows := e.events.all(); len(rows) != 1 || rows[0].Type != "impression" {
		t.Fatalf("rows: %+v", rows)
	}
}

func BenchmarkRequestAd(b *testing.B) {
	e := newEnv(b)
	body, _ := json.Marshal(adReq())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Post(e.srv.URL+"/v1/ad", "application/json", bytes.NewReader(body))
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
