package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/opportunity"
	"github.com/RDowse/arb-scanner/internal/storage"
)

// stubStore records the filter it was asked for and returns what the test set.
type stubStore struct {
	records    []storage.Record
	strategies []string
	err        error

	gotFilter storage.Filter
}

func (s *stubStore) List(_ context.Context, f storage.Filter) ([]storage.Record, error) {
	s.gotFilter = f
	return s.records, s.err
}

func (s *stubStore) Strategies(context.Context) ([]string, error) {
	return s.strategies, s.err
}

func record(id string) storage.Record {
	seen := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	return storage.Record{
		ID:         id,
		RouteID:    "route-" + id,
		Strategy:   "cross_venue",
		ConfigID:   "v1",
		ObservedAt: seen,
		StartAsset: "USD",
		AmountIn:   decimal.NewFromInt(10000),
		AmountOut:  decimal.NewFromInt(10100),
		NetProfit:  decimal.NewFromInt(100),
		NetEdgeBps: decimal.NewFromInt(120),
		Legs:       []opportunity.Leg{{Venue: "kraken", Side: opportunity.Buy}},
	}
}

func serve(store Store, target string) *httptest.ResponseRecorder {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := httptest.NewRecorder()
	New(store, log).Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()

	var body T
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return body
}

func TestListOpportunities(t *testing.T) {
	t.Run("returns the stored records as json", func(t *testing.T) {
		store := &stubStore{records: []storage.Record{record("abc")}}

		w := serve(store, "/opportunities")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
		}
		if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("content type = %q", got)
		}

		got := decodeBody[opportunityPage](t, w)
		if len(got.Opportunities) != 1 || got.Opportunities[0].ID != "abc" {
			t.Fatalf("body = %s", w.Body)
		}
		if got.Count != 1 {
			t.Errorf("count = %d, want 1", got.Count)
		}
		if got.Limit != 100 {
			t.Errorf("limit = %d, want the default 100", got.Limit)
		}
		if !got.Opportunities[0].NetEdgeBps.Equal(decimal.NewFromInt(120)) {
			t.Errorf("net edge = %s, want 120: decimals must survive the round trip", got.Opportunities[0].NetEdgeBps)
		}
	})

	t.Run("renders no results as an empty array", func(t *testing.T) {
		w := serve(&stubStore{records: []storage.Record{}}, "/opportunities")

		if body := w.Body.String(); !strings.Contains(body, `"opportunities":[]`) {
			t.Errorf("body = %q, want an empty array inside the envelope", body)
		}
	})

	t.Run("reports the limit it applied, not the one asked for", func(t *testing.T) {
		w := serve(&stubStore{}, "/opportunities?limit=5000")

		if got := decodeBody[opportunityPage](t, w); got.Limit != 1000 {
			t.Errorf("limit = %d, want the 1000 cap", got.Limit)
		}
	})

	t.Run("passes the filter through", func(t *testing.T) {
		store := &stubStore{}

		w := serve(store, "/opportunities?strategy=triangular&from=2026-09-06T12:00:00Z&to=2026-09-06T13:00:00Z&limit=5")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
		}

		want := storage.Filter{
			Strategy: "triangular",
			From:     time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
			To:       time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC),
			Limit:    5,
		}
		if store.gotFilter.Strategy != want.Strategy || store.gotFilter.Limit != want.Limit {
			t.Errorf("filter = %+v, want %+v", store.gotFilter, want)
		}
		if !store.gotFilter.From.Equal(want.From) || !store.gotFilter.To.Equal(want.To) {
			t.Errorf("window = %s..%s, want %s..%s", store.gotFilter.From, store.gotFilter.To, want.From, want.To)
		}
	})

	t.Run("rejects malformed query parameters", func(t *testing.T) {
		tests := []struct {
			name   string
			target string
		}{
			{"from is not a timestamp", "/opportunities?from=yesterday"},
			{"to is not a timestamp", "/opportunities?to=2026-13-45"},
			{"limit is not a number", "/opportunities?limit=lots"},
			{"limit is zero", "/opportunities?limit=0"},
			{"limit is negative", "/opportunities?limit=-5"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				w := serve(&stubStore{}, tt.target)
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400: %s", w.Code, w.Body)
				}
				if got := decodeBody[map[string]string](t, w); got["error"] == "" {
					t.Error("no error message in the body")
				}
			})
		}
	})

	t.Run("reports a store failure without leaking it", func(t *testing.T) {
		w := serve(&stubStore{err: errors.New("connection refused to db-prod-1")}, "/opportunities")

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
		if body := w.Body.String(); body == "" || strings.Contains(body, "db-prod-1") {
			t.Errorf("body = %q, want a generic message", body)
		}
	})
}

func TestListStrategies(t *testing.T) {
	w := serve(&stubStore{strategies: []string{"cross_venue", "triangular"}}, "/strategies")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
	got := decodeBody[strategyList](t, w)
	if len(got.Strategies) != 2 || got.Strategies[0] != "cross_venue" || got.Strategies[1] != "triangular" {
		t.Errorf("body = %s", w.Body)
	}
	if got.Count != 2 {
		t.Errorf("count = %d, want 2", got.Count)
	}
}

func TestHealth(t *testing.T) {
	w := serve(&stubStore{}, "/health")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := decodeBody[map[string]string](t, w); got["status"] != "ok" {
		t.Errorf("body = %s", w.Body)
	}
}

func TestRouting(t *testing.T) {
	t.Run("unknown path is 404", func(t *testing.T) {
		if w := serve(&stubStore{}, "/nope"); w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("wrong method is 405", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/opportunities", nil)

		New(&stubStore{}, log).Handler().ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", w.Code)
		}
	})
}
