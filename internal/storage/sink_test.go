//go:build db

package storage

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

type stored struct {
	routeID    string
	observedAt time.Time
	amountOut  decimal.Decimal
	legs       string
}

func read(t *testing.T, db *Postgres, id string) stored {
	t.Helper()

	var s stored
	err := db.pool.QueryRow(context.Background(), `
		SELECT route_id, observed_at, amount_out, legs::text
		FROM opportunities WHERE id = $1`, id).
		Scan(&s.routeID, &s.observedAt, &s.amountOut, &s.legs)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return s
}

func count(t *testing.T, db *Postgres, where string, args ...any) int {
	t.Helper()

	var n int
	if err := db.pool.QueryRow(context.Background(), "SELECT count(*) FROM opportunities WHERE "+where, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestPostgresStore(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	firstTick := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	t.Run("stores nothing for an empty tick", func(t *testing.T) {
		if err := db.Store(ctx, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	wide := route(t, firstTick, "10100")
	if err := db.Store(ctx, []opportunity.Opportunity{wide}); err != nil {
		t.Fatalf("store: %v", err)
	}

	t.Run("inserts a sighting", func(t *testing.T) {
		if n := count(t, db, "true"); n != 1 {
			t.Errorf("row count = %d, want 1", n)
		}

		got := read(t, db, wide.ID)
		if got.routeID != wide.RouteID {
			t.Errorf("route id = %s, want %s", got.routeID, wide.RouteID)
		}
		if !got.observedAt.Equal(firstTick) {
			t.Errorf("observed at %s, want %s", got.observedAt, firstTick)
		}
		if !got.amountOut.Equal(dec("10100")) {
			t.Errorf("amount out = %s, want 10100", got.amountOut)
		}
		if got.legs == "" {
			t.Error("legs are empty")
		}
	})

	t.Run("adds a row when the same route is seen again", func(t *testing.T) {
		secondTick := firstTick.Add(time.Second)
		narrow := route(t, secondTick, "10050")
		if narrow.RouteID != wide.RouteID {
			t.Fatalf("route id changed with the price: %s then %s", wide.RouteID, narrow.RouteID)
		}
		if narrow.ID == wide.ID {
			t.Fatalf("two sightings share a sighting id: %s", narrow.ID)
		}
		if err := db.Store(ctx, []opportunity.Opportunity{narrow}); err != nil {
			t.Fatalf("store: %v", err)
		}

		if n := count(t, db, "route_id = $1", wide.RouteID); n != 2 {
			t.Errorf("route rows = %d, want 2: a later sighting must not replace the earlier one", n)
		}
		if got := read(t, db, wide.ID); !got.amountOut.Equal(dec("10100")) {
			t.Errorf("earlier amount out = %s, want the original 10100 untouched", got.amountOut)
		}
		if got := read(t, db, narrow.ID); !got.amountOut.Equal(dec("10050")) {
			t.Errorf("later amount out = %s, want 10050", got.amountOut)
		}
	})

	t.Run("re-storing a tick changes nothing", func(t *testing.T) {
		before := count(t, db, "true")
		if err := db.Store(ctx, []opportunity.Opportunity{wide}); err != nil {
			t.Fatalf("store: %v", err)
		}
		if after := count(t, db, "true"); after != before {
			t.Errorf("row count = %d, want the unchanged %d", after, before)
		}
	})

	t.Run("stores a batch of routes atomically", func(t *testing.T) {
		fourthTick := firstTick.Add(3 * time.Second)
		other := routeFor(t, "cross_venue", "binance", "10300", fourthTick)

		if err := db.Store(ctx, []opportunity.Opportunity{route(t, fourthTick, "10250"), other}); err != nil {
			t.Fatalf("store: %v", err)
		}
		if n := count(t, db, "observed_at = $1", fourthTick); n != 2 {
			t.Errorf("rows for the tick = %d, want 2", n)
		}
	})
}
