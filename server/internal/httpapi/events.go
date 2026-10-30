package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Shtrihan/OpenAd/server/internal/chwriter"
	"github.com/Shtrihan/OpenAd/server/internal/state"
	"github.com/Shtrihan/OpenAd/server/internal/token"
)

const maxBatch = 50

var eventTypes = map[string]bool{
	"loaded": true, "impression": true, "viewable": true, "click": true, "close": true,
	"video_start": true, "video_q1": true, "video_mid": true, "video_q3": true, "video_complete": true,
	"reward": true, "error": true,
}

type eventIn struct {
	Type       string    `json:"type"`
	EventToken string    `json:"event_token"`
	TS         time.Time `json:"ts"`
	Data       struct {
		Code       int    `json:"code"`
		Message    string `json:"message"`
		VisibleMs  int64  `json:"visible_ms"`
		PlayedMs   int64  `json:"played_ms"`
		UserID     string `json:"user_id"`
		CustomData string `json:"custom_data"`
	} `json:"data"`
}

type eventResult struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

var (
	errUnavailable  = errors.New("state store unavailable")
	errBackpressure = errors.New("event buffer full")
)

func (s *Server) trackEvents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Events []eventIn `json:"events"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	if err := dec.Decode(&body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "batch too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "body must be a JSON event batch")
		return
	}
	if len(body.Events) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "events must not be empty")
		return
	}
	if len(body.Events) > maxBatch {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "at most 50 events per batch")
		return
	}
	snap := s.deps.Catalog.Snapshot()
	if snap == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "catalog not loaded yet")
		return
	}
	// An event marked as seen in Redis but not buffered would be lost for
	// good (retries are deduplicated), so refuse the batch up front instead.
	if s.deps.Events.Free() < len(body.Events) {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "unavailable", "temporarily unavailable; retry the batch")
		return
	}

	results := make([]eventResult, len(body.Events))
	for i := range body.Events {
		res, err := s.processEvent(r, snap.Keyring, &body.Events[i])
		if err != nil {
			// Already processed events are deduplicated, so the SDK can resend the whole batch.
			s.log.Error("event processing failed", "err", err)
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, "unavailable", "temporarily unavailable; retry the batch")
			return
		}
		results[i] = res
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// pixel handles VAST tracking URLs. It always answers 204 so players never
// retry (see the API contract); rejections are only logged.
func (s *Server) pixel(w http.ResponseWriter, r *http.Request) {
	defer func() {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}()
	snap := s.deps.Catalog.Snapshot()
	if snap == nil || s.deps.Events.Free() == 0 {
		return
	}
	q := r.URL.Query()
	ev := eventIn{Type: q.Get("e"), EventToken: q.Get("tk"), TS: s.deps.Now()}
	if ec := q.Get("ec"); ec != "" {
		ev.Data.Code, _ = strconv.Atoi(ec)
	}
	res, err := s.processEvent(r, snap.Keyring, &ev)
	if err != nil {
		s.log.Error("pixel event failed", "err", err)
	} else if res.Status == "rejected" {
		s.log.Info("pixel event rejected", "type", ev.Type, "reason", res.Reason)
	}
}

func rejected(reason string) eventResult { return eventResult{Status: "rejected", Reason: reason} }

func (s *Server) processEvent(r *http.Request, keys *token.Keyring, ev *eventIn) (eventResult, error) {
	now := s.deps.Now()
	if !eventTypes[ev.Type] {
		return rejected("unknown_type"), nil
	}
	c, err := keys.Verify(ev.EventToken, now)
	switch {
	case errors.Is(err, token.ErrExpired):
		return rejected("expired"), nil
	case err != nil:
		return rejected("invalid_token"), nil
	}
	if len(ev.Data.Message) > 512 || len(ev.Data.UserID) > 256 || len(ev.Data.CustomData) > 1024 ||
		ev.Data.VisibleMs < 0 || ev.Data.PlayedMs < 0 || ev.Data.Code < 0 || ev.Data.Code > 65535 {
		return rejected("invalid_data"), nil
	}
	// The ad may only be shown inside its show window.
	if ev.Type == "impression" && now.Unix() > c.ShowUntil {
		return rejected("expired"), nil
	}

	res, err := s.deps.State.Record(r.Context(), c, ev.Type, now)
	if err != nil {
		return eventResult{}, errors.Join(errUnavailable, err)
	}
	switch res {
	case state.Duplicate:
		return eventResult{Status: "duplicate"}, nil
	case state.OutOfOrder:
		return rejected("out_of_order"), nil
	}

	clientTS := ev.TS
	if clientTS.IsZero() {
		clientTS = now
	}
	if !s.deps.Events.Add(eventRow(c, ev, now, clientTS)) {
		return eventResult{}, errBackpressure
	}
	return eventResult{Status: "accepted"}, nil
}

func eventRow(c *token.Claims, ev *eventIn, now, clientTS time.Time) chwriter.Event {
	id := func(s string) uuid.UUID { u, _ := uuid.Parse(s); return u }
	clamp := func(v int64) uint32 { return uint32(min(max(v, 0), 1<<32-1)) }
	return chwriter.Event{
		ReceivedAt: now, ClientTS: clientTS.UTC(), Type: ev.Type,
		AdID: id(c.AdID), RequestID: id(c.RequestID), Test: c.Test,
		PublisherID: id(c.PublisherID), AppID: id(c.AppID), PlacementID: id(c.PlacementID), Format: c.Format,
		AdvertiserID: id(c.AdvertiserID), CampaignID: id(c.CampaignID), CreativeID: id(c.CreativeID), CreativeType: c.CreativeType,
		Platform: c.Platform, OS: c.OS, Country: c.Country, Language: c.Language,
		SDKName: c.SDKName, SDKVersion: c.SDKVersion, AppVersion: c.AppVersion,
		InstallationID: id(c.InstallationID), IPHash: c.IPHash,
		ErrorCode: uint16(ev.Data.Code), VisibleMs: clamp(ev.Data.VisibleMs), PlayedMs: clamp(ev.Data.PlayedMs),
	}
}
