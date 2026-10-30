package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	k1 = Key{ID: "k1", Secret: []byte("0123456789abcdef0123456789abcdef")}
	k2 = Key{ID: "k2", Secret: []byte("fedcba9876543210fedcba9876543210")}
)

func claims(now time.Time) *Claims {
	return &Claims{
		AdID: "ad-1", CampaignID: "c-1", InstallationID: "i-1", Platform: "web",
		IssuedAt: now.UnixMilli(), ShowUntil: now.Add(time.Hour).Unix(), ExpiresAt: now.Add(7 * time.Hour).Unix(),
	}
}

func TestRoundTrip(t *testing.T) {
	now := time.Now()
	ring := NewKeyring(k1)
	tok, err := ring.Sign(claims(now))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ring.Verify(tok, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.AdID != "ad-1" || got.CampaignID != "c-1" {
		t.Fatalf("claims: %+v", got)
	}
}

func TestRotation(t *testing.T) {
	now := time.Now()
	old, _ := NewKeyring(k1).Sign(claims(now))
	rotated := NewKeyring(k2, k1)
	if _, err := rotated.Verify(old, now); err != nil {
		t.Fatalf("token signed with previous key must still verify: %v", err)
	}
	if _, err := NewKeyring(k2).Verify(old, now); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("retired key: got %v", err)
	}
}

func TestRejects(t *testing.T) {
	now := time.Now()
	ring := NewKeyring(k1)
	tok, _ := ring.Sign(claims(now))
	parts := strings.Split(tok, ".")

	forged := NewKeyring(Key{ID: "k1", Secret: []byte("attacker-secret-attacker-secret!!")})
	forgedTok, _ := forged.Sign(claims(now))

	// Payload swapped from another token, original MAC kept.
	other := claims(now)
	other.CampaignID = "c-2"
	otherTok, _ := ring.Sign(other)
	swapped := strings.Join([]string{parts[0], parts[1], strings.Split(otherTok, ".")[2], parts[3]}, ".")

	cases := []struct {
		name string
		tok  string
		now  time.Time
		want error
	}{
		{"empty", "", now, ErrMalformed},
		{"garbage", "not-a-token", now, ErrMalformed},
		{"wrong version", "v0" + tok[2:], now, ErrMalformed},
		{"forged secret", forgedTok, now, ErrSignature},
		{"swapped payload", swapped, now, ErrSignature},
		{"expired", tok, now.Add(8 * time.Hour), ErrExpired},
	}
	for _, c := range cases {
		if _, err := ring.Verify(c.tok, c.now); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

func BenchmarkSignVerify(b *testing.B) {
	now := time.Now()
	ring := NewKeyring(k1)
	c := claims(now)
	for i := 0; i < b.N; i++ {
		tok, _ := ring.Sign(c)
		if _, err := ring.Verify(tok, now); err != nil {
			b.Fatal(err)
		}
	}
}

func TestNonCanonicalEncodingRejected(t *testing.T) {
	now := time.Now()
	ring := NewKeyring(k1)
	tok, _ := ring.Sign(claims(now))
	// A 32-byte MAC is 43 base64 chars; the last char carries 2 padding bits.
	// Changing only those bits must not produce another valid token.
	last := tok[len(tok)-1]
	for _, c := range "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_" {
		if byte(c) == last {
			continue
		}
		if _, err := ring.Verify(tok[:len(tok)-1]+string(c), now); err == nil {
			t.Fatalf("variant ending in %q accepted", c)
		}
	}
}
