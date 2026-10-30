package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(checks map[string]Check) *httptest.Server {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httptest.NewServer(New("test", checks, log).Handler())
}

func ok(context.Context) error   { return nil }
func down(context.Context) error { return errors.New("connection refused") }

func get(t *testing.T, url string) (*http.Response, health) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var h health
	if resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
			t.Fatal(err)
		}
	}
	return resp, h
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(map[string]Check{"postgres": down})
	defer srv.Close()

	resp, h := get(t, srv.URL+"/healthz")
	if resp.StatusCode != http.StatusOK || h.Status != "ok" || h.Version != "test" {
		t.Fatalf("healthz: %d %+v", resp.StatusCode, h)
	}
}

func TestReadyz(t *testing.T) {
	srv := newTestServer(map[string]Check{"postgres": ok, "redis": ok})
	defer srv.Close()
	resp, h := get(t, srv.URL+"/readyz")
	if resp.StatusCode != http.StatusOK || h.Checks["redis"] != "ok" {
		t.Fatalf("readyz all ok: %d %+v", resp.StatusCode, h)
	}

	srv2 := newTestServer(map[string]Check{"postgres": ok, "redis": down})
	defer srv2.Close()
	resp, h = get(t, srv2.URL+"/readyz")
	if resp.StatusCode != http.StatusServiceUnavailable || h.Status != "down" || h.Checks["redis"] != "down" {
		t.Fatalf("readyz with failure: %d %+v", resp.StatusCode, h)
	}
}

func TestPixelAlways204(t *testing.T) {
	srv := newTestServer(nil)
	defer srv.Close()
	resp, _ := get(t, srv.URL+"/v1/t?e=impression&tk=garbage")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("pixel: %d", resp.StatusCode)
	}
}

func TestServingNotImplementedYet(t *testing.T) {
	srv := newTestServer(nil)
	defer srv.Close()
	for _, path := range []string{"/v1/ad", "/v1/events"} {
		resp, err := http.Post(srv.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
}
