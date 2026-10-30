// Package selector chooses which ad to show. It is pure: all state comes in
// through arguments, so it is fast and fully unit-testable.
package selector

import (
	"math/rand/v2"
	"time"

	"github.com/Shtrihan/OpenAd/server/internal/catalog"
)

// Request is the targeting context of one ad request.
type Request struct {
	Placement *catalog.Placement
	Platform  string
	Country   string // ISO 3166-1 alpha-2, upper case; "" if unknown
	Language  string // ISO 639-1, lower case
	Video     bool   // client can play video
	Now       time.Time
}

// Eligible returns campaigns that pass targeting for req, in catalog order.
// Frequency caps are applied separately because they need Redis.
func Eligible(snap *catalog.Snapshot, req Request) []*catalog.Campaign {
	var out []*catalog.Campaign
	for _, c := range snap.Campaigns {
		if !demandAllows(req.Placement, c) ||
			snap.AppBlocked(c.AppID) ||
			!c.Active(req.Now) ||
			!match(c.Formats, req.Placement.Format) ||
			!match(c.Platforms, req.Platform) ||
			!match(c.Countries, req.Country) ||
			!match(c.Languages, req.Language) ||
			!hasCompatibleCreative(c, req) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Pick chooses a campaign with probability proportional to its weight,
// skipping excluded ones, then a compatible creative uniformly at random.
// It returns nils when nothing is left.
func Pick(cands []*catalog.Campaign, req Request, excluded func(*catalog.Campaign) bool, intN func(int) int) (*catalog.Campaign, *catalog.Creative) {
	if intN == nil {
		intN = rand.IntN
	}
	total := 0
	pool := cands[:0:0]
	for _, c := range cands {
		if excluded != nil && excluded(c) {
			continue
		}
		pool = append(pool, c)
		total += c.Weight
	}
	if total == 0 {
		return nil, nil
	}
	n := intN(total)
	for _, c := range pool {
		if n < c.Weight {
			return c, pickCreative(c, req, intN)
		}
		n -= c.Weight
	}
	return nil, nil // unreachable
}

func pickCreative(c *catalog.Campaign, req Request, intN func(int) int) *catalog.Creative {
	var compatible []*catalog.Creative
	for _, cr := range c.Creatives {
		if creativeFits(cr, req) {
			compatible = append(compatible, cr)
		}
	}
	return compatible[intN(len(compatible))]
}

func hasCompatibleCreative(c *catalog.Campaign, req Request) bool {
	for _, cr := range c.Creatives {
		if creativeFits(cr, req) {
			return true
		}
	}
	return false
}

func creativeFits(cr *catalog.Creative, req Request) bool {
	if cr.Type != "video" {
		return true
	}
	p := req.Placement
	if !req.Video || !p.AllowVideo {
		return false
	}
	return p.MaxVideoDurationS == 0 || cr.DurationMs <= p.MaxVideoDurationS*1000
}

// demandAllows applies the portfolio-first rule: an app never promotes
// itself; other members' campaigns are shown only if the placement opted in.
func demandAllows(p *catalog.Placement, c *catalog.Campaign) bool {
	if c.AppID == p.AppID {
		return false
	}
	return c.MemberID == p.MemberID || p.Demand == "portfolio_and_circle"
}

// match treats an empty set as "any value", including unknown ("").
func match(set map[string]bool, v string) bool {
	return len(set) == 0 || set[v]
}
