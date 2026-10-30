// Package token signs and verifies event tokens.
//
// A token is issued with every served ad and carries everything needed to
// record its events, so the event path needs no lookups:
//
//	v1.<key id>.<base64url(JSON claims)>.<base64url(HMAC-SHA256)>
//
// The MAC covers everything before the last dot. Several keys can be valid at
// once so keys can be rotated without invalidating ads already in flight.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const version = "v1"

var (
	ErrMalformed  = errors.New("token: malformed")
	ErrUnknownKey = errors.New("token: unknown key")
	ErrSignature  = errors.New("token: bad signature")
	ErrExpired    = errors.New("token: expired")
)

// Claims describe one served ad. Short JSON names keep tokens small.
type Claims struct {
	AdID         string `json:"ad"`
	RequestID    string `json:"rq"`
	PublisherID  string `json:"pm"` // member that owns the app showing the ad
	AppID        string `json:"ap"`
	PlacementID  string `json:"pl"`
	Format       string `json:"fm"`
	AdvertiserID string `json:"am"` // member that owns the campaign
	CampaignID   string `json:"cp"`
	CreativeID   string `json:"cr"`
	CreativeType string `json:"ct"`

	InstallationID string `json:"in"`
	SessionID      string `json:"ss,omitempty"`
	Platform       string `json:"pf"`
	OS             string `json:"os,omitempty"`
	Country        string `json:"cc,omitempty"`
	Language       string `json:"lg,omitempty"`
	SDKName        string `json:"sn"`
	SDKVersion     string `json:"sv"`
	AppVersion     string `json:"av,omitempty"`
	IPHash         uint64 `json:"ih,omitempty"`
	Test           bool   `json:"t,omitempty"`

	RewardName   string `json:"rn,omitempty"`
	RewardAmount int    `json:"ra,omitempty"`

	IssuedAt  int64 `json:"iat"` // unix milliseconds
	ShowUntil int64 `json:"su"`  // unix seconds; the ad must be shown before this
	ExpiresAt int64 `json:"exp"` // unix seconds; events are rejected after this
}

// Key is one HMAC secret identified by a short id.
type Key struct {
	ID     string
	Secret []byte
}

// Keyring signs with the active key and verifies with any known key.
// It is immutable; build a new one to rotate keys.
type Keyring struct {
	active Key
	keys   map[string][]byte
}

// NewKeyring returns a keyring that signs with active and also accepts others.
func NewKeyring(active Key, others ...Key) *Keyring {
	k := &Keyring{active: active, keys: map[string][]byte{active.ID: active.Secret}}
	for _, o := range others {
		k.keys[o.ID] = o.Secret
	}
	return k
}

// Strict decoding rejects non-canonical encodings, so a token has exactly one valid spelling.
var b64 = base64.RawURLEncoding.Strict()

// Sign encodes and signs claims with the active key.
func (k *Keyring) Sign(c *Claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.Grow(len(version) + len(k.active.ID) + b64.EncodedLen(len(payload)) + 48)
	sb.WriteString(version)
	sb.WriteByte('.')
	sb.WriteString(k.active.ID)
	sb.WriteByte('.')
	sb.WriteString(b64.EncodeToString(payload))
	mac := sum(k.active.Secret, sb.String())
	sb.WriteByte('.')
	sb.WriteString(b64.EncodeToString(mac))
	return sb.String(), nil
}

// Verify checks the signature and expiry and returns the claims.
func (k *Keyring) Verify(tok string, now time.Time) (*Claims, error) {
	if len(tok) > 4096 {
		return nil, ErrMalformed
	}
	dot := strings.LastIndexByte(tok, '.')
	if dot < 0 {
		return nil, ErrMalformed
	}
	signed, macPart := tok[:dot], tok[dot+1:]
	parts := strings.Split(signed, ".")
	if len(parts) != 3 || parts[0] != version {
		return nil, ErrMalformed
	}
	secret, ok := k.keys[parts[1]]
	if !ok {
		return nil, ErrUnknownKey
	}
	mac, err := b64.DecodeString(macPart)
	if err != nil {
		return nil, ErrMalformed
	}
	if !hmac.Equal(mac, sum(secret, signed)) {
		return nil, ErrSignature
	}
	payload, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, ErrMalformed
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, ErrMalformed
	}
	if now.Unix() > c.ExpiresAt {
		return nil, ErrExpired
	}
	return &c, nil
}

func sum(secret []byte, msg string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(msg))
	return h.Sum(nil)
}
