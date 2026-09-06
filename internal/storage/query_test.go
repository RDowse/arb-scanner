//go:build db

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

// seed stores three routes across two strategies, each seen at a different
// time, so ordering and filtering have something to separate.
func seed(t *testing.T, db *Postgres) (crossEarly, crossLate, triangular opportunity.Opportunity) {
	t.Helper()

	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	crossEarly = route(t, base, "10100")
	crossLate = route(t, base.Add(2*time.Minute), "10200")
	crossLate.Legs[1].Venue = "binance"
	crossLate.ID = crossLate.RouteID()

	triangular = route(t, base.Add(time.Minute), "10150")
	triangular.Strategy = "triangular"
	triangular.ID = triangular.RouteID()

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

	t.Run("returns the most recently seen first", func(t *testing.T) {
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
			t.Fatalf("got %v, want just the route seen inside the window", ids(got))
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

	t.Run("carries the sighting window and best edge", func(t *testing.T) {
		got, err := db.List(ctx, Filter{Strategy: "cross_venue", Limit: 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d records, want 1", len(got))
		}

		r := got[0]
		if !r.FirstSeenAt.Equal(crossLate.ObservedAt) || !r.LastSeenAt.Equal(crossLate.ObservedAt) {
			t.Errorf("seen at %s..%s, want both %s", r.FirstSeenAt, r.LastSeenAt, crossLate.ObservedAt)
		}
		if !r.MaxEdgeBps.Equal(crossLate.NetEdgeBps) {
			t.Errorf("max edge = %s, want %s", r.MaxEdgeBps, crossLate.NetEdgeBps)
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

func TestPostgresGet(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	_, crossLate, _ := seed(t, db)

	t.Run("returns the route by id", func(t *testing.T) {
		got, err := db.Get(ctx, crossLate.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != crossLate.ID {
			t.Errorf("id = %s, want %s", got.ID, crossLate.ID)
		}
		if !got.NetProfit.Equal(crossLate.NetProfit) {
			t.Errorf("net profit = %s, want %s", got.NetProfit, crossLate.NetProfit)
		}
	})

	t.Run("reports a missing id", func(t *testing.T) {
		_, err := db.Get(ctx, "0000000000000000")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
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
