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
	firstSeenAt time.Time
	lastSeenAt  time.Time
	netEdgeBps  decimal.Decimal
	maxEdgeBps  decimal.Decimal
	amountOut   decimal.Decimal
	legs        string
	count       int
}

func read(t *testing.T, db *Postgres, id string) stored {
	t.Helper()

	var s stored
	err := db.pool.QueryRow(context.Background(), `
		SELECT first_seen_at, last_seen_at, net_edge_bps, max_edge_bps, amount_out, legs::text,
		       (SELECT count(*) FROM opportunities)
		FROM opportunities WHERE id = $1`, id).
		Scan(&s.firstSeenAt, &s.lastSeenAt, &s.netEdgeBps, &s.maxEdgeBps, &s.amountOut, &s.legs, &s.count)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return s
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

	best := route(t, firstTick, "10100")
	if err := db.Store(ctx, []opportunity.Opportunity{best}); err != nil {
		t.Fatalf("store: %v", err)
	}

	t.Run("inserts a new route", func(t *testing.T) {
		got := read(t, db, best.ID)
		if got.count != 1 {
			t.Errorf("row count = %d, want 1", got.count)
		}
		if !got.firstSeenAt.Equal(firstTick) || !got.lastSeenAt.Equal(firstTick) {
			t.Errorf("seen at %s..%s, want both %s", got.firstSeenAt, got.lastSeenAt, firstTick)
		}
		if !got.maxEdgeBps.Equal(got.netEdgeBps) {
			t.Errorf("max edge %s, want the inserted %s", got.maxEdgeBps, got.netEdgeBps)
		}
		if !got.amountOut.Equal(dec("10100")) {
			t.Errorf("amount out = %s, want 10100", got.amountOut)
		}
	})

	t.Run("keeps the best observation when the edge narrows", func(t *testing.T) {
		secondTick := firstTick.Add(time.Second)
		worse := route(t, secondTick, "10050")
		if worse.ID != best.ID {
			t.Fatalf("route id changed with the price: %s then %s", best.ID, worse.ID)
		}
		if err := db.Store(ctx, []opportunity.Opportunity{worse}); err != nil {
			t.Fatalf("store: %v", err)
		}

		got := read(t, db, best.ID)
		if got.count != 1 {
			t.Errorf("row count = %d, want 1: the same route must not open a second row", got.count)
		}
		if !got.firstSeenAt.Equal(firstTick) {
			t.Errorf("first seen at %s, want the original %s", got.firstSeenAt, firstTick)
		}
		if !got.lastSeenAt.Equal(secondTick) {
			t.Errorf("last seen at %s, want %s", got.lastSeenAt, secondTick)
		}
		if !got.amountOut.Equal(dec("10100")) {
			t.Errorf("amount out = %s, want the better 10100 retained", got.amountOut)
		}
		if !got.maxEdgeBps.Equal(best.NetEdgeBps) {
			t.Errorf("max edge = %s, want the better %s retained", got.maxEdgeBps, best.NetEdgeBps)
		}
	})

	t.Run("replaces the observation when the edge widens", func(t *testing.T) {
		thirdTick := firstTick.Add(2 * time.Second)
		better := route(t, thirdTick, "10200")
		if err := db.Store(ctx, []opportunity.Opportunity{better}); err != nil {
			t.Fatalf("store: %v", err)
		}

		got := read(t, db, best.ID)
		if got.count != 1 {
			t.Errorf("row count = %d, want 1", got.count)
		}
		if !got.lastSeenAt.Equal(thirdTick) {
			t.Errorf("last seen at %s, want %s", got.lastSeenAt, thirdTick)
		}
		if !got.amountOut.Equal(dec("10200")) {
			t.Errorf("amount out = %s, want the new 10200", got.amountOut)
		}
		if !got.maxEdgeBps.Equal(better.NetEdgeBps) {
			t.Errorf("max edge = %s, want %s", got.maxEdgeBps, better.NetEdgeBps)
		}
		if got.legs == "" {
			t.Error("legs are empty")
		}
	})

	t.Run("stores a batch of routes atomically", func(t *testing.T) {
		fourthTick := firstTick.Add(3 * time.Second)
		other := route(t, fourthTick, "10300")
		other.Legs[1].Venue = "binance"
		other.ID = other.RouteID()

		if err := db.Store(ctx, []opportunity.Opportunity{route(t, fourthTick, "10250"), other}); err != nil {
			t.Fatalf("store: %v", err)
		}
		if got := read(t, db, other.ID); got.count != 2 {
			t.Errorf("row count = %d, want 2", got.count)
		}
	})
}
