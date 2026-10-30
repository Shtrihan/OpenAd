package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/Shtrihan/OpenAd/server/internal/catalog"
	"github.com/Shtrihan/OpenAd/server/internal/chwriter"
	"github.com/Shtrihan/OpenAd/server/internal/selector"
	"github.com/Shtrihan/OpenAd/server/internal/token"
)

type adRequest struct {
	ID          string `json:"id"`
	PlacementID string `json:"placement_id"`
	App         struct {
		ID      string `json:"id"`
		Bundle  string `json:"bundle"`
		Version string `json:"version"`
	} `json:"app"`
	Device struct {
		Platform string `json:"platform"`
		OS       string `json:"os"`
		Language string `json:"language"`
	} `json:"device"`
	User struct {
		InstallationID string `json:"installation_id"`
		SessionID      string `json:"session_id"`
	} `json:"user"`
	SDK struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"sdk"`
	Capabilities struct {
		Video *bool `json:"video"`
	} `json:"capabilities"`
	Test bool `json:"test"`
}

var (
	platforms = map[string]bool{"web": true, "android": true, "ios": true, "desktop": true}
	sdkNames  = map[string]bool{
		"openad-web": true, "openad-android": true, "openad-ios": true,
		"openad-flutter": true, "openad-react-native": true, "openad-unity": true,
	}
	langRe = regexp.MustCompile(`^[a-z]{2,3}$`)
)

func (r *adRequest) validate() string {
	for _, id := range []string{r.ID, r.PlacementID, r.App.ID, r.User.InstallationID} {
		if _, err := uuid.Parse(id); err != nil {
			return "id, placement_id, app.id and user.installation_id must be UUIDs"
		}
	}
	if r.User.SessionID != "" {
		if _, err := uuid.Parse(r.User.SessionID); err != nil {
			return "user.session_id must be a UUID"
		}
	}
	switch {
	case !platforms[r.Device.Platform]:
		return "device.platform is invalid"
	case !langRe.MatchString(r.Device.Language):
		return "device.language must be an ISO 639 code in lower case"
	case !sdkNames[r.SDK.Name] || r.SDK.Version == "" || len(r.SDK.Version) > 32:
		return "sdk.name or sdk.version is invalid"
	case len(r.App.Bundle) > 255 || len(r.App.Version) > 64 || len(r.Device.OS) > 64:
		return "field too long"
	}
	return ""
}

type adResponse struct {
	RequestID string `json:"request_id"`
	Ad        adBody `json:"ad"`
}

type adBody struct {
	ID         string     `json:"id"`
	Format     string     `json:"format"`
	Creative   creative   `json:"creative"`
	ClickURL   string     `json:"click_url,omitempty"`
	EventToken string     `json:"event_token"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Close      closeBody  `json:"close"`
	Reward     *reward    `json:"reward,omitempty"`
	Disclosure disclosure `json:"disclosure"`
}

type creative struct {
	ID    string  `json:"id"`
	Type  string  `json:"type"`
	Image []image `json:"image,omitempty"`
}

type image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Mime   string `json:"mime"`
}

type closeBody struct {
	DelayMs int `json:"delay_ms"`
}

type reward struct {
	Name   string `json:"name"`
	Amount int    `json:"amount"`
}

type disclosure struct {
	Label      string `json:"label"`
	Advertiser string `json:"advertiser,omitempty"`
	Erid       string `json:"erid,omitempty"`
}

// adLabel is the localized ad marker shown on every creative.
func adLabel(lang string) string {
	switch lang {
	case "ru", "uk", "be", "kk", "bg", "sr":
		return "Реклама"
	default:
		return "Ad"
	}
}

func (s *Server) requestAd(w http.ResponseWriter, r *http.Request) {
	start := s.deps.Now()

	var req adRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "body must be a JSON ad request")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}

	snap := s.deps.Catalog.Snapshot()
	if snap == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "catalog not loaded yet")
		return
	}
	p := snap.Placements[req.PlacementID]
	if p == nil || p.AppID != req.App.ID {
		writeError(w, http.StatusNotFound, "unknown_placement", "placement not found for this app")
		return
	}

	ip := s.deps.Geo.ClientIP(r)
	rec := chwriter.Request{
		ReceivedAt:     start,
		RequestID:      uuid.MustParse(req.ID),
		Test:           req.Test,
		PublisherID:    uuid.MustParse(p.MemberID),
		AppID:          uuid.MustParse(p.AppID),
		PlacementID:    uuid.MustParse(p.ID),
		Format:         p.Format,
		Platform:       req.Device.Platform,
		Country:        s.deps.Geo.Country(r, ip),
		Language:       req.Device.Language,
		SDKVersion:     req.SDK.Version,
		InstallationID: uuid.MustParse(req.User.InstallationID),
	}
	noFill := func(reason string) {
		rec.NoFillReason = reason
		rec.LatencyUs = uint32(s.deps.Now().Sub(start).Microseconds())
		s.deps.Requests.Add(rec)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-OpenAd-No-Fill-Reason", reason)
		w.WriteHeader(http.StatusNoContent)
	}

	if snap.InstallationBlocked(req.User.InstallationID) || snap.IPBlocked(ip.String()) || snap.AppBlocked(p.AppID) {
		noFill("blocked")
		return
	}

	sel := selector.Request{
		Placement: p,
		Platform:  req.Device.Platform,
		Country:   rec.Country,
		Language:  req.Device.Language,
		Video:     req.Capabilities.Video == nil || *req.Capabilities.Video,
		Now:       start,
	}
	cands := selector.Eligible(snap, sel)
	if len(cands) == 0 {
		noFill("no_eligible_ads")
		return
	}

	caps, err := s.deps.State.CheckCaps(r.Context(), p, req.User.InstallationID, req.User.SessionID, cands, start)
	if err != nil {
		// Without caps we cannot protect the user, so don't serve.
		s.log.Error("frequency cap check failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "temporarily unavailable")
		return
	}
	if caps.PlacementCapped {
		noFill("frequency_cap")
		return
	}
	camp, cr := selector.Pick(cands, sel, func(c *catalog.Campaign) bool { return caps.Campaigns[c.ID] }, nil)
	if camp == nil {
		noFill("frequency_cap")
		return
	}

	adID := uuid.New()
	showUntil := start.Add(s.deps.ShowWindow)
	claims := &token.Claims{
		AdID: adID.String(), RequestID: req.ID,
		PublisherID: p.MemberID, AppID: p.AppID, PlacementID: p.ID, Format: p.Format,
		AdvertiserID: camp.MemberID, CampaignID: camp.ID, CreativeID: cr.ID, CreativeType: cr.Type,
		InstallationID: req.User.InstallationID, SessionID: req.User.SessionID,
		Platform: req.Device.Platform, OS: req.Device.OS, Country: rec.Country, Language: req.Device.Language,
		SDKName: req.SDK.Name, SDKVersion: req.SDK.Version, AppVersion: req.App.Version,
		IPHash: s.deps.Geo.HashIP(ip), Test: req.Test,
		RewardName: p.RewardName, RewardAmount: p.RewardAmount,
		IssuedAt: start.UnixMilli(), ShowUntil: showUntil.Unix(),
		ExpiresAt: showUntil.Add(s.deps.EventGrace).Unix(),
	}
	tok, err := snap.Keyring.Sign(claims)
	if err != nil {
		s.log.Error("sign token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	resp := adResponse{RequestID: req.ID, Ad: adBody{
		ID:         adID.String(),
		Format:     p.Format,
		Creative:   creative{ID: cr.ID, Type: cr.Type},
		ClickURL:   cr.ClickURL,
		EventToken: tok,
		ExpiresAt:  showUntil.UTC().Truncate(time.Second),
		Close:      closeBody{DelayMs: p.CloseDelayMs},
		Disclosure: disclosure{Label: adLabel(req.Device.Language), Advertiser: cr.AdvertiserLabel, Erid: cr.Erid},
	}}
	for _, rd := range cr.Renditions {
		resp.Ad.Creative.Image = append(resp.Ad.Creative.Image, image{URL: rd.URL, Width: rd.Width, Height: rd.Height, Mime: rd.Mime})
	}
	if p.Format == "rewarded" {
		resp.Ad.Reward = &reward{Name: p.RewardName, Amount: p.RewardAmount}
	}

	rec.Filled = true
	rec.AdID = adID
	rec.CampaignID, _ = uuid.Parse(camp.ID)
	rec.CreativeID, _ = uuid.Parse(cr.ID)
	rec.LatencyUs = uint32(s.deps.Now().Sub(start).Microseconds())
	s.deps.Requests.Add(rec) // best effort: request logs may be dropped under pressure
	writeJSON(w, http.StatusOK, resp)
}
