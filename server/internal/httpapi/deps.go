package httpapi

import (
	"context"
	"time"

	"github.com/Shtrihan/OpenAd/server/internal/catalog"
	"github.com/Shtrihan/OpenAd/server/internal/chwriter"
	"github.com/Shtrihan/OpenAd/server/internal/geo"
	"github.com/Shtrihan/OpenAd/server/internal/state"
	"github.com/Shtrihan/OpenAd/server/internal/token"
)

// Deps are the collaborators of the serving handlers.
type Deps struct {
	Catalog  interface{ Snapshot() *catalog.Snapshot }
	State    State
	Events   Sink[chwriter.Event]
	Requests Sink[chwriter.Request]
	Geo      *geo.Resolver

	Now        func() time.Time
	ShowWindow time.Duration // how long a served ad may wait before being shown
	EventGrace time.Duration // how long after ShowWindow events are still accepted
}

type State interface {
	CheckCaps(ctx context.Context, p *catalog.Placement, inst, session string, cands []*catalog.Campaign, now time.Time) (state.Caps, error)
	Record(ctx context.Context, c *token.Claims, typ string, now time.Time) (state.Result, error)
}

type Sink[T any] interface {
	Add(rows ...T) bool
	Free() int // rows that can still be added
}
