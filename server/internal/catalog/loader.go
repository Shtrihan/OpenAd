package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Shtrihan/OpenAd/server/internal/token"
)

// Store holds the current snapshot and refreshes it when the catalog changes.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	cur  atomic.Pointer[Snapshot]

	// Periodic reload as a safety net in case a notification is missed.
	RefreshInterval time.Duration
}

func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	return &Store{pool: pool, log: log, RefreshInterval: time.Minute}
}

// Snapshot returns the current snapshot, or nil before the first load.
func (s *Store) Snapshot() *Snapshot { return s.cur.Load() }

// Load builds a fresh snapshot and swaps it in.
func (s *Store) Load(ctx context.Context) error {
	start := time.Now()
	snap, err := load(ctx, s.pool)
	if err != nil {
		return err
	}
	s.cur.Store(snap)
	s.log.Info("catalog loaded",
		"placements", len(snap.Placements),
		"campaigns", len(snap.Campaigns),
		"duration_ms", time.Since(start).Milliseconds())
	return nil
}

// Run reloads on `NOTIFY openad_catalog` (debounced) and periodically,
// until ctx is cancelled. Listen errors are retried with backoff.
func (s *Store) Run(ctx context.Context) {
	changed := make(chan struct{}, 1)
	go s.listen(ctx, changed)

	const debounce = 200 * time.Millisecond
	ticker := time.NewTicker(s.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-changed:
			// Coalesce bursts of changes (e.g. a dashboard save touching several tables).
			select {
			case <-time.After(debounce):
			case <-ctx.Done():
				return
			}
		}
		if err := s.Load(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("catalog reload failed; serving previous snapshot", "err", err)
		}
	}
}

func (s *Store) listen(ctx context.Context, changed chan<- struct{}) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := s.listenOnce(ctx, changed)
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("catalog listener disconnected", "err", err, "retry_in", backoff.String())
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (s *Store) listenOnce(ctx context.Context, changed chan<- struct{}) error {
	pooled, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection carries LISTEN state, so take it out of the pool for
	// good. After Hijack the pooled handle is invalid; use only conn.
	conn := pooled.Hijack()
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN openad_catalog"); err != nil {
		return err
	}
	// Changes made while we were disconnected would otherwise be missed.
	select {
	case changed <- struct{}{}:
	default:
	}
	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return err
		}
		select {
		case changed <- struct{}{}:
		default:
		}
	}
}

// EnsureSigningKey creates a signing key if none is active, so a fresh
// deployment works without manual setup.
func EnsureSigningKey(ctx context.Context, pool *pgxpool.Pool) error {
	id := make([]byte, 4)
	secret := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO signing_keys (id, secret)
		SELECT $1, $2
		WHERE NOT EXISTS (SELECT 1 FROM signing_keys WHERE active AND retired_at IS NULL)`,
		hex.EncodeToString(id), secret)
	return err
}

func load(ctx context.Context, pool *pgxpool.Pool) (*Snapshot, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	snap := &Snapshot{LoadedAt: time.Now(), Placements: map[string]*Placement{}}
	steps := []struct {
		name string
		fn   func(context.Context, pgx.Tx, *Snapshot) error
	}{
		{"placements", loadPlacements},
		{"campaigns", loadCampaigns},
		{"keys", loadKeys},
		{"blocklist", loadBlocklist},
	}
	for _, st := range steps {
		if err := st.fn(ctx, tx, snap); err != nil {
			return nil, fmt.Errorf("load %s: %w", st.name, err)
		}
	}
	return snap, nil
}

func loadPlacements(ctx context.Context, tx pgx.Tx, snap *Snapshot) error {
	rows, err := tx.Query(ctx, `
		SELECT p.id::text, a.id::text, a.member_id::text, a.platform::text, a.bundle, p.format::text, p.demand::text,
		       coalesce(p.min_interval_s, 0), coalesce(p.max_per_session, 0), coalesce(p.max_per_day, 0),
		       p.close_delay_ms, p.allow_video, coalesce(p.max_video_duration_s, 0),
		       coalesce(p.reward_name, ''), coalesce(p.reward_amount, 0)
		FROM placements p
		JOIN apps a    ON a.id = p.app_id
		JOIN members m ON m.id = a.member_id
		WHERE p.status = 'active' AND a.status = 'active' AND m.status = 'active'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p Placement
		if err := rows.Scan(&p.ID, &p.AppID, &p.MemberID, &p.Platform, &p.Bundle, &p.Format, &p.Demand,
			&p.MinIntervalS, &p.MaxPerSession, &p.MaxPerDay,
			&p.CloseDelayMs, &p.AllowVideo, &p.MaxVideoDurationS,
			&p.RewardName, &p.RewardAmount); err != nil {
			return err
		}
		snap.Placements[p.ID] = &p
	}
	return rows.Err()
}

func loadCampaigns(ctx context.Context, tx pgx.Tx, snap *Snapshot) error {
	rows, err := tx.Query(ctx, `
		SELECT c.id::text, c.member_id::text, c.app_id::text, c.weight,
		       c.formats::text[], c.platforms::text[], c.countries::text[], c.languages,
		       coalesce(c.max_per_user_day, 0), c.starts_at, c.ends_at
		FROM campaigns c
		JOIN members m ON m.id = c.member_id
		JOIN apps a    ON a.id = c.app_id
		WHERE c.status = 'active' AND m.status = 'active' AND a.status = 'active'
		ORDER BY c.id`)
	if err != nil {
		return err
	}
	byID := map[string]*Campaign{}
	var all []*Campaign
	for rows.Next() {
		var (
			c                                        Campaign
			formats, platforms, countries, languages []string
			startsAt, endsAt                         *time.Time
		)
		if err := rows.Scan(&c.ID, &c.MemberID, &c.AppID, &c.Weight,
			&formats, &platforms, &countries, &languages,
			&c.MaxPerUserDay, &startsAt, &endsAt); err != nil {
			rows.Close()
			return err
		}
		c.Formats, c.Platforms = set(formats), set(platforms)
		c.Countries, c.Languages = set(countries), set(languages)
		if startsAt != nil {
			c.StartsAt = *startsAt
		}
		if endsAt != nil {
			c.EndsAt = *endsAt
		}
		byID[c.ID] = &c
		all = append(all, &c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Video creatives are served from stage 3 (VAST), once the transcoding
	// pipeline produces renditions; until then only images are loaded.
	creatives := map[string]*Creative{}
	rows, err = tx.Query(ctx, `
		SELECT cr.id::text, cr.campaign_id::text, cr.type::text, cr.click_url,
		       coalesce(cr.advertiser_label, ''), coalesce(cr.erid, ''), coalesce(cr.duration_ms, 0)
		FROM creatives cr
		WHERE cr.status = 'ready' AND cr.type = 'image'
		ORDER BY cr.id`)
	if err != nil {
		return err
	}
	type pending struct {
		campaignID string
		cr         *Creative
	}
	var order []pending
	for rows.Next() {
		var (
			cr         Creative
			campaignID string
		)
		if err := rows.Scan(&cr.ID, &campaignID, &cr.Type, &cr.ClickURL,
			&cr.AdvertiserLabel, &cr.Erid, &cr.DurationMs); err != nil {
			rows.Close()
			return err
		}
		creatives[cr.ID] = &cr
		order = append(order, pending{campaignID, &cr})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `
		SELECT r.creative_id::text, r.url, r.mime, r.width, r.height
		FROM creative_renditions r
		JOIN creatives cr ON cr.id = r.creative_id
		WHERE cr.status = 'ready'
		ORDER BY r.creative_id, r.width * r.height`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			creativeID string
			r          Rendition
		)
		if err := rows.Scan(&creativeID, &r.URL, &r.Mime, &r.Width, &r.Height); err != nil {
			rows.Close()
			return err
		}
		if cr := creatives[creativeID]; cr != nil {
			cr.Renditions = append(cr.Renditions, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Only creatives with at least one file can be served.
	for _, p := range order {
		if c := byID[p.campaignID]; c != nil && len(p.cr.Renditions) > 0 {
			c.Creatives = append(c.Creatives, p.cr)
		}
	}
	for _, c := range all {
		if len(c.Creatives) > 0 {
			snap.Campaigns = append(snap.Campaigns, c)
		}
	}
	return nil
}

func loadKeys(ctx context.Context, tx pgx.Tx, snap *Snapshot) error {
	rows, err := tx.Query(ctx, `
		SELECT id, secret, active
		FROM signing_keys
		WHERE retired_at IS NULL OR retired_at > now()
		ORDER BY active DESC, created_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var keys []token.Key
	var hasActive bool
	for rows.Next() {
		var (
			k      token.Key
			active bool
		)
		if err := rows.Scan(&k.ID, &k.Secret, &active); err != nil {
			return err
		}
		if len(keys) == 0 {
			hasActive = active
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasActive {
		return errors.New("no active signing key")
	}
	snap.Keyring = token.NewKeyring(keys[0], keys[1:]...)
	return nil
}

func loadBlocklist(ctx context.Context, tx pgx.Tx, snap *Snapshot) error {
	rows, err := tx.Query(ctx, `
		SELECT kind, value FROM blocklist WHERE expires_at IS NULL OR expires_at > now()`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return err
		}
		snap.Block(kind, value)
	}
	return rows.Err()
}

func set(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}
