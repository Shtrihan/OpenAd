// Package catalog keeps an in-memory snapshot of everything the ad server
// needs to answer a request: placements, campaigns, creatives, signing keys
// and the blocklist. The request path never queries PostgreSQL.
package catalog

import (
	"time"

	"github.com/Shtrihan/OpenAd/server/internal/token"
)

type Placement struct {
	ID       string
	AppID    string
	MemberID string // publisher
	Platform string // platform of the app
	Bundle   string
	Format   string
	// Demand is "portfolio" (only the owner's other apps) or
	// "portfolio_and_circle" (also campaigns of other members).
	Demand string

	MinIntervalS      int // 0 = no cap
	MaxPerSession     int
	MaxPerDay         int
	CloseDelayMs      int
	AllowVideo        bool
	MaxVideoDurationS int

	RewardName   string
	RewardAmount int
}

type Campaign struct {
	ID       string
	MemberID string // advertiser
	AppID    string // promoted app
	Weight   int

	// Empty set means "any".
	Formats   map[string]bool
	Platforms map[string]bool
	Countries map[string]bool
	Languages map[string]bool

	MaxPerUserDay int
	StartsAt      time.Time // zero = no start limit
	EndsAt        time.Time // zero = no end limit

	Creatives []*Creative
}

// Active reports whether the campaign's schedule includes now.
func (c *Campaign) Active(now time.Time) bool {
	if !c.StartsAt.IsZero() && now.Before(c.StartsAt) {
		return false
	}
	if !c.EndsAt.IsZero() && !now.Before(c.EndsAt) {
		return false
	}
	return true
}

type Creative struct {
	ID              string
	Type            string // image | video
	ClickURL        string
	AdvertiserLabel string
	Erid            string
	DurationMs      int
	Renditions      []Rendition
}

type Rendition struct {
	URL    string
	Mime   string
	Width  int
	Height int
}

// Snapshot is an immutable view of the catalog. Readers must not modify it.
type Snapshot struct {
	LoadedAt   time.Time
	Placements map[string]*Placement
	Campaigns  []*Campaign
	Keyring    *token.Keyring

	blockedInstallations map[string]bool
	blockedIPs           map[string]bool
	blockedApps          map[string]bool
}

func (s *Snapshot) InstallationBlocked(id string) bool { return s.blockedInstallations[id] }
func (s *Snapshot) IPBlocked(ip string) bool           { return s.blockedIPs[ip] }
func (s *Snapshot) AppBlocked(id string) bool          { return s.blockedApps[id] }

// Blocklist builds the blocklist part of a snapshot; used by the loader and tests.
func (s *Snapshot) Block(kind, value string) {
	m := map[string]*map[string]bool{
		"installation": &s.blockedInstallations,
		"ip":           &s.blockedIPs,
		"app":          &s.blockedApps,
	}[kind]
	if m == nil {
		return
	}
	if *m == nil {
		*m = map[string]bool{}
	}
	(*m)[value] = true
}
