//go:build db

package storage

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

const defaultTestDatabaseURL = "postgres://arb:arb@localhost:5432/arb_test?sslmode=disable"

// Postgres-backed tests. Excluded from the default build:
//
//	./scripts/test.sh -tags=db -run TestPostgres -v
//
// TEST_DATABASE_URL overrides the database they run against.
func open(t *testing.T) *Postgres {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = defaultTestDatabaseURL
	}
	ensureDatabase(t, url)

	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(db.Close)

	applySchema(t, db)
	return db
}

func ensureDatabase(t *testing.T, url string) {
	t.Helper()

	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse %s: %v", url, err)
	}
	target := cfg.Database

	cfg.Database = "postgres"
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Skipf("no postgres at %s:%d: %v", cfg.Host, cfg.Port, err)
	}
	defer conn.Close(ctx)

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", target).Scan(&exists); err != nil {
		t.Fatalf("look up %s: %v", target, err)
	}
	if exists {
		return
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{target}.Sanitize()); err != nil {
		t.Fatalf("create %s: %v", target, err)
	}
}

func applySchema(t *testing.T, db *Postgres) {
	t.Helper()

	ctx := context.Background()
	if _, err := db.pool.Exec(ctx, "DROP TABLE IF EXISTS opportunities, schema_migrations"); err != nil {
		t.Fatalf("drop schema: %v", err)
	}

	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	sort.Strings(files)

	for _, file := range files {
		stmt, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if _, err := db.pool.Exec(ctx, string(stmt)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
}

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func route(t *testing.T, observedAt time.Time, amountOut string) opportunity.Opportunity {
	t.Helper()

	legs := []opportunity.Leg{
		{
			Venue: "kraken", Symbol: "BTC/USD", Side: opportunity.Buy,
			Base: "BTC", Quote: "USD", AssetIn: "USD", AssetOut: "BTC",
			AmountIn: dec("10000"), AmountOut: dec("0.2"), Price: dec("50000"), Qty: dec("0.2"),
		},
		{
			Venue: "coinbase", Symbol: "BTC/USD", Side: opportunity.Sell,
			Base: "BTC", Quote: "USD", AssetIn: "BTC", AssetOut: "USD",
			AmountIn: dec("0.2"), AmountOut: dec(amountOut), Price: dec("50500"), Qty: dec("0.2"),
		},
	}

	o, err := opportunity.New("cross_venue", "v1", observedAt, dec("10000"), dec(amountOut), dec("66"), legs)
	if err != nil {
		t.Fatalf("build opportunity: %v", err)
	}
	return o
}

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
