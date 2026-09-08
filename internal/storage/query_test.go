//go:build db

package storage

import (
	"context"
	"testing"
	"time"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

// seed stores three sightings across two strategies, each observed at a
// different time, so ordering and filtering have something to separate.
func seed(t *testing.T, db *Postgres) (crossEarly, crossLate, triangular opportunity.Opportunity) {
	t.Helper()

	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	crossEarly = route(t, base, "10100")
	crossLate = routeFor(t, "cross_venue", "binance", "10200", base.Add(2*time.Minute))
	triangular = routeFor(t, "triangular", "coinbase", "10150", base.Add(time.Minute))

	if err := db.Store(context.Background(), []opportunity.Opportunity{crossEarly, crossLate, triangular}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return crossEarly, crossLate, triangular
}

func ids(records []Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.ID)
	}
	return out
}

func TestPostgresList(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	crossEarly, crossLate, triangular := seed(t, db)

	t.Run("returns the most recently observed first", func(t *testing.T) {
		got, err := db.List(ctx, Filter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{crossLate.ID, triangular.ID, crossEarly.ID}
		if len(got) != len(want) {
			t.Fatalf("got %d records, want %d", len(got), len(want))
		}
		for i, id := range want {
			if got[i].ID != id {
				t.Fatalf("order = %v, want %v", ids(got), want)
			}
		}
	})

	t.Run("filters by strategy", func(t *testing.T) {
		got, err := db.List(ctx, Filter{Strategy: "triangular"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != triangular.ID {
			t.Fatalf("got %v, want just %s", ids(got), triangular.ID)
		}
	})

	t.Run("filters by time window", func(t *testing.T) {
		from := crossEarly.ObservedAt.Add(30 * time.Second)
		to := crossLate.ObservedAt.Add(-30 * time.Second)

		got, err := db.List(ctx, Filter{From: from, To: to})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != triangular.ID {
			t.Fatalf("got %v, want just the sighting inside the window", ids(got))
		}
	})

	t.Run("applies the limit", func(t *testing.T) {
		got, err := db.List(ctx, Filter{Limit: 2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d records, want 2", len(got))
		}
	})

	t.Run("returns an empty page rather than nil", func(t *testing.T) {
		got, err := db.List(ctx, Filter{Strategy: "no_such_strategy"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Error("got nil, want an empty slice so the API renders []")
		}
	})

	t.Run("carries the observation and its legs", func(t *testing.T) {
		got, err := db.List(ctx, Filter{Strategy: "cross_venue", Limit: 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d records, want 1", len(got))
		}

		r := got[0]
		if !r.ObservedAt.Equal(crossLate.ObservedAt) {
			t.Errorf("observed at %s, want %s", r.ObservedAt, crossLate.ObservedAt)
		}
		if r.RouteID != crossLate.RouteID {
			t.Errorf("route id = %s, want %s", r.RouteID, crossLate.RouteID)
		}
		if !r.NetEdgeBps.Equal(crossLate.NetEdgeBps) {
			t.Errorf("net edge = %s, want %s", r.NetEdgeBps, crossLate.NetEdgeBps)
		}
		if len(r.Legs) != 2 {
			t.Fatalf("got %d legs, want 2", len(r.Legs))
		}
		if r.Legs[0].Side != opportunity.Buy || r.Legs[1].Side != opportunity.Sell {
			t.Errorf("legs = %s then %s, want buy then sell", r.Legs[0].Side, r.Legs[1].Side)
		}
		if !r.Legs[0].Price.Equal(dec("50000")) {
			t.Errorf("leg 0 price = %s, want 50000: jsonb decimals must survive the round trip", r.Legs[0].Price)
		}
	})
}

// A route seen on successive ticks is a series, not a single row that moves.
func TestPostgresListRouteHistory(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	var want []string
	var opps []opportunity.Opportunity
	for i, amountOut := range []string{"10200", "10150", "10100"} {
		o := route(t, base.Add(time.Duration(i)*time.Second), amountOut)
		opps = append(opps, o)
		want = append(want, o.ID)
	}
	if err := db.Store(ctx, opps); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := db.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sightings, want %d: every tick a route pays is its own row", len(got), len(want))
	}

	// Newest first, so the decaying edge reads back in reverse.
	for i, id := range []string{want[2], want[1], want[0]} {
		if got[i].ID != id {
			t.Fatalf("order = %v, want newest first", ids(got))
		}
		if got[i].RouteID != opps[0].RouteID {
			t.Errorf("route id = %s, want every sighting to share %s", got[i].RouteID, opps[0].RouteID)
		}
	}
	if !got[0].NetEdgeBps.LessThan(got[2].NetEdgeBps) {
		t.Errorf("newest edge %s is not below the oldest %s", got[0].NetEdgeBps, got[2].NetEdgeBps)
	}
}

func TestFilterEffectiveLimit(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"unset falls back to the default", 0, defaultLimit},
		{"negative falls back to the default", -1, defaultLimit},
		{"in range is kept", 25, 25},
		{"over the cap is clamped", maxLimit + 1, maxLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Filter{Limit: tt.in}).EffectiveLimit(); got != tt.want {
				t.Errorf("limit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPostgresStrategies(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	seed(t, db)

	got, err := db.Strategies(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"cross_venue", "triangular"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
