// Package httpapi exposes the public ad serving API described in api/openapi.yaml.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Check reports whether one dependency is reachable.
type Check func(ctx context.Context) error

// Server routes HTTP requests.
type Server struct {
	version string
	checks  map[string]Check
	log     *slog.Logger
	deps    Deps
}

func New(version string, checks map[string]Check, log *slog.Logger, deps Deps) *Server {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.ShowWindow == 0 {
		deps.ShowWindow = time.Hour
	}
	if deps.EventGrace == 0 {
		deps.EventGrace = 6 * time.Hour
	}
	return &Server{version: version, checks: checks, log: log, deps: deps}
}

// Handler returns the root handler with all routes and middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("POST /v1/ad", s.requestAd)
	mux.HandleFunc("POST /v1/events", s.trackEvents)
	mux.HandleFunc("GET /v1/t", s.pixel)
	return s.recoverer(s.accessLog(mux))
}

type health struct {
	Status  string            `json:"status"`
	Version string            `json:"version,omitempty"`
	Checks  map[string]string `json:"checks,omitempty"`
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, health{Status: "ok", Version: s.version})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]string, len(s.checks))
	)
	for name, check := range s.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := "ok"
			if err := check(ctx); err != nil {
				status = "down"
				s.log.Warn("readiness check failed", "check", name, "err", err)
			}
			mu.Lock()
			results[name] = status
			mu.Unlock()
		}()
	}
	wg.Wait()

	resp, code := health{Status: "ok", Version: s.version, Checks: results}, http.StatusOK
	for _, st := range results {
		if st != "ok" {
			resp.Status, code = "down", http.StatusServiceUnavailable
			break
		}
	}
	writeJSON(w, code, resp)
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var e apiError
	e.Error.Code, e.Error.Message = code, msg
	writeJSON(w, status, e)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// CheckNames returns the configured check names in stable order (for logging).
func (s *Server) CheckNames() []string {
	names := make([]string, 0, len(s.checks))
	for n := range s.checks {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
