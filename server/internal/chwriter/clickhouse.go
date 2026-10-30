package chwriter

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// Event is one row of the events table.
type Event struct {
	ReceivedAt, ClientTS time.Time
	Type                 string
	AdID, RequestID      uuid.UUID
	Test                 bool

	PublisherID, AppID, PlacementID uuid.UUID
	Format                          string

	AdvertiserID, CampaignID, CreativeID uuid.UUID
	CreativeType                         string

	Platform, OS, Country, Language string
	SDKName, SDKVersion, AppVersion string
	InstallationID                  uuid.UUID
	IPHash                          uint64

	ErrorCode           uint16
	VisibleMs, PlayedMs uint32
}

// Request is one row of the ad_requests table.
type Request struct {
	ReceivedAt time.Time
	RequestID  uuid.UUID
	Test       bool

	PublisherID, AppID, PlacementID uuid.UUID
	Format                          string

	Platform, Country, Language, SDKVersion string
	InstallationID                          uuid.UUID

	Filled                       bool
	NoFillReason                 string
	AdID, CampaignID, CreativeID uuid.UUID
	LatencyUs                    uint32
}

// Open connects to ClickHouse over the native protocol.
func Open(addr, database, user, password string) (driver.Conn, error) {
	return clickhouse.Open(&clickhouse.Options{
		Addr:        []string{addr},
		Auth:        clickhouse.Auth{Database: database, Username: user, Password: password},
		Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
		DialTimeout: 5 * time.Second,
	})
}

func InsertEvents(conn driver.Conn) func(context.Context, []Event) error {
	return func(ctx context.Context, rows []Event) error {
		batch, err := conn.PrepareBatch(ctx, `INSERT INTO events (
			received_at, client_ts, event_type, ad_id, request_id, test,
			publisher_member_id, app_id, placement_id, format,
			advertiser_member_id, campaign_id, creative_id, creative_type,
			platform, os, country, language, sdk_name, sdk_version, app_version,
			installation_id, ip_hash, error_code, visible_ms, played_ms)`)
		if err != nil {
			return err
		}
		defer batch.Abort()
		for _, r := range rows {
			if err := batch.Append(
				r.ReceivedAt, r.ClientTS, r.Type, r.AdID, r.RequestID, r.Test,
				r.PublisherID, r.AppID, r.PlacementID, r.Format,
				r.AdvertiserID, r.CampaignID, r.CreativeID, r.CreativeType,
				r.Platform, r.OS, r.Country, r.Language, r.SDKName, r.SDKVersion, r.AppVersion,
				r.InstallationID, r.IPHash, r.ErrorCode, r.VisibleMs, r.PlayedMs,
			); err != nil {
				return err
			}
		}
		return batch.Send()
	}
}

func InsertRequests(conn driver.Conn) func(context.Context, []Request) error {
	return func(ctx context.Context, rows []Request) error {
		batch, err := conn.PrepareBatch(ctx, `INSERT INTO ad_requests (
			received_at, request_id, test,
			publisher_member_id, app_id, placement_id, format,
			platform, country, language, sdk_version, installation_id,
			filled, no_fill_reason, ad_id, campaign_id, creative_id, latency_us)`)
		if err != nil {
			return err
		}
		defer batch.Abort()
		for _, r := range rows {
			if err := batch.Append(
				r.ReceivedAt, r.RequestID, r.Test,
				r.PublisherID, r.AppID, r.PlacementID, r.Format,
				r.Platform, r.Country, r.Language, r.SDKVersion, r.InstallationID,
				r.Filled, r.NoFillReason, r.AdID, r.CampaignID, r.CreativeID, r.LatencyUs,
			); err != nil {
				return err
			}
		}
		return batch.Send()
	}
}
