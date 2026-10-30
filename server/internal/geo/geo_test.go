package geo

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	r := NewResolver([]byte("k"))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.7")

	if got := r.ClientIP(req).String(); got != "10.0.0.1" {
		t.Fatalf("untrusted proxy: %s", got)
	}
	r.TrustProxy = true
	if got := r.ClientIP(req).String(); got != "203.0.113.7" {
		t.Fatalf("trusted proxy: %s", got)
	}
	req.Header.Set("X-Real-IP", "198.51.100.2")
	if got := r.ClientIP(req).String(); got != "198.51.100.2" {
		t.Fatalf("x-real-ip: %s", got)
	}
}

func TestCountryHeader(t *testing.T) {
	r := NewResolver([]byte("k"))
	r.CountryHeader = "CF-IPCountry"
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("CF-IPCountry", "ru")
	if got := r.Country(req, r.ClientIP(req)); got != "RU" {
		t.Fatalf("got %q", got)
	}
	req.Header.Set("CF-IPCountry", "XX") // Cloudflare: unknown
	if got := r.Country(req, r.ClientIP(req)); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestHashIPIsKeyed(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	a, b := NewResolver([]byte("a")), NewResolver([]byte("b"))
	ip := a.ClientIP(req)
	if a.HashIP(ip) == b.HashIP(ip) || a.HashIP(ip) != a.HashIP(ip) {
		t.Fatal("hash must be stable per key and differ across keys")
	}
}
