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

// route builds a valid two-leg cross-venue route, priced so amountOut decides
// the edge.
func route(t *testing.T, observedAt time.Time, amountOut string) opportunity.Opportunity {
	return routeFor(t, "cross_venue", "coinbase", amountOut, observedAt)
}

// routeFor varies the parts the route id hashes, so a test can build a route
// that differs in identity rather than only in price.
func routeFor(t *testing.T, strategy, sellVenue, amountOut string, observedAt time.Time) opportunity.Opportunity {
	t.Helper()

	legs := []opportunity.Leg{
		{
			Venue: "kraken", Symbol: "BTC/USD", Side: opportunity.Buy,
			Base: "BTC", Quote: "USD", AssetIn: "USD", AssetOut: "BTC",
			AmountIn: dec("10000"), AmountOut: dec("0.2"), Price: dec("50000"), Qty: dec("0.2"),
		},
		{
			Venue: sellVenue, Symbol: "BTC/USD", Side: opportunity.Sell,
			Base: "BTC", Quote: "USD", AssetIn: "BTC", AssetOut: "USD",
			AmountIn: dec("0.2"), AmountOut: dec(amountOut), Price: dec("50500"), Qty: dec("0.2"),
		},
	}

	o, err := opportunity.New(strategy, "v1", observedAt, dec("10000"), dec(amountOut), dec("66"), legs)
	if err != nil {
		t.Fatalf("build opportunity: %v", err)
	}
	return o
}
