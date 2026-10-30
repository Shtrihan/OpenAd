// Package geo resolves the client IP and its country.
package geo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/oschwald/maxminddb-golang"
)

// Resolver determines client IP and country for a request.
type Resolver struct {
	// TrustProxy uses X-Forwarded-For / X-Real-IP set by a reverse proxy.
	// Enable only when the server is reachable solely through that proxy.
	TrustProxy bool
	// CountryHeader, if set, is read first (e.g. CF-IPCountry from Cloudflare).
	CountryHeader string
	// DB is an optional MaxMind-format country database (GeoLite2 or DB-IP Lite).
	DB *maxminddb.Reader

	hashKey []byte
}

func NewResolver(hashKey []byte) *Resolver { return &Resolver{hashKey: hashKey} }

// OpenDB opens a MaxMind-format database file.
func OpenDB(path string) (*maxminddb.Reader, error) { return maxminddb.Open(path) }

// ClientIP returns the IP of the end user.
func (r *Resolver) ClientIP(req *http.Request) netip.Addr {
	if r.TrustProxy {
		if v := strings.TrimSpace(req.Header.Get("X-Real-IP")); v != "" {
			if ip, err := netip.ParseAddr(v); err == nil {
				return ip.Unmap()
			}
		}
		// The proxy appends the address it saw, so the last entry is the one we trust.
		if v := req.Header.Get("X-Forwarded-For"); v != "" {
			parts := strings.Split(v, ",")
			if ip, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				return ip.Unmap()
			}
		}
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	ip, _ := netip.ParseAddr(host)
	return ip.Unmap()
}

// Country returns the ISO 3166-1 alpha-2 code in upper case, or "".
func (r *Resolver) Country(req *http.Request, ip netip.Addr) string {
	if r.CountryHeader != "" {
		if cc := strings.ToUpper(strings.TrimSpace(req.Header.Get(r.CountryHeader))); len(cc) == 2 && cc != "XX" {
			return cc
		}
	}
	if r.DB == nil || !ip.IsValid() {
		return ""
	}
	var rec struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := r.DB.Lookup(net.IP(ip.AsSlice()), &rec); err != nil {
		return ""
	}
	return strings.ToUpper(rec.Country.ISOCode)
}

// HashIP returns a keyed hash so raw IPs are never stored.
func (r *Resolver) HashIP(ip netip.Addr) uint64 {
	if !ip.IsValid() {
		return 0
	}
	h := hmac.New(sha256.New, r.hashKey)
	b := ip.As16()
	h.Write(b[:])
	return binary.BigEndian.Uint64(h.Sum(nil))
}
