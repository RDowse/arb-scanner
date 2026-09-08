package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/RDowse/arb-scanner/internal/storage"
)

// Store is the read side of the opportunity store, satisfied by
// *storage.Postgres.
type Store interface {
	List(ctx context.Context, f storage.Filter) ([]storage.Record, error)
	Strategies(ctx context.Context) ([]string, error)
}

// opportunityPage wraps the collection so pagination metadata can be added
// without changing the shape clients already parse.
type opportunityPage struct {
	Opportunities []storage.Record `json:"opportunities"`
	Count         int              `json:"count"`
	Limit         int              `json:"limit"`
}

type strategyList struct {
	Strategies []string `json:"strategies"`
	Count      int      `json:"count"`
}

type Server struct {
	store Store
	log   *slog.Logger
}

func New(store Store, log *slog.Logger) *Server {
	return &Server{store: store, log: log}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /opportunities", s.listOpportunities)
	mux.HandleFunc("GET /strategies", s.listStrategies)
	mux.HandleFunc("GET /health", s.health)
	return s.logRequests(mux)
}

func (s *Server) listOpportunities(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	records, err := s.store.List(r.Context(), filter)
	if err != nil {
		s.log.Error("list opportunities", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read opportunities")
		return
	}
	writeJSON(w, http.StatusOK, opportunityPage{
		Opportunities: records,
		Count:         len(records),
		Limit:         filter.EffectiveLimit(),
	})
}

func (s *Server) listStrategies(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.Strategies(r.Context())
	if err != nil {
		s.log.Error("list strategies", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read strategies")
		return
	}
	writeJSON(w, http.StatusOK, strategyList{Strategies: names, Count: len(names)})
}

// health reports that the process is serving. Per-venue feed age arrives with
// the venue_feed_status table.
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseFilter reads the query string, rejecting malformed input rather than
// silently falling back, so a client mistyping a timestamp is told about it.
func parseFilter(q url.Values) (storage.Filter, error) {
	filter := storage.Filter{Strategy: q.Get("strategy")}

	for _, bound := range []struct {
		name string
		into *time.Time
	}{
		{"from", &filter.From},
		{"to", &filter.To},
	} {
		raw := q.Get(bound.name)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return storage.Filter{}, errors.New(bound.name + " must be an RFC3339 timestamp")
		}
		*bound.into = t
	}

	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return storage.Filter{}, errors.New("limit must be a positive integer")
		}
		filter.Limit = limit
	}

	return filter, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so the client sees a truncated body.
		slog.Default().Error("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", recorder.status,
			"duration", time.Since(started),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
