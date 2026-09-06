package strategy

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
)

var evaluatedAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func level(price, size string) market.Level {
	return market.Level{Price: dec(price), Size: dec(size)}
}

func book(venue, symbol string, bids, asks []market.Level) market.Book {
	return market.Book{Venue: venue, Symbol: symbol, Bids: bids, Asks: asks, UpdatedAt: evaluatedAt}
}

// strategy trades kraken and coinbase at their lowest-tier taker fees.
func strategy() *CrossVenue {
	return &CrossVenue{
		ConfigID: "v1",
		TakerFees: map[string]decimal.Decimal{
			"kraken":   dec("0.0026"),
			"coinbase": dec("0.0040"),
		},
		MaxBookAge: 5 * time.Second,
		Now:        func() time.Time { return evaluatedAt },
	}
}

// freeStrategy removes fees so the arithmetic in a test is the book's alone.
func freeStrategy() *CrossVenue {
	s := strategy()
	s.TakerFees = map[string]decimal.Decimal{"kraken": decimal.Zero, "coinbase": decimal.Zero}
	return s
}

func TestCrossVenueGenerate(t *testing.T) {
	t.Run("emits a route that clears both fees", func(t *testing.T) {
		s := strategy()
		books := []market.Book{
			book("kraken", "BTC/USD", []market.Level{level("49900", "1")}, []market.Level{level("50000", "1")}),
			book("coinbase", "BTC/USD", []market.Level{level("50500", "1")}, []market.Level{level("50600", "1")}),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 1 {
			t.Fatalf("got %d opportunities, want 1", len(opps))
		}

		got := opps[0]
		// cost 50000 + 130 fee in, proceeds 50500 - 202 fee out.
		if want := dec("50130"); !got.AmountIn.Equal(want) {
			t.Errorf("AmountIn = %s, want %s", got.AmountIn, want)
		}
		if want := dec("50298"); !got.AmountOut.Equal(want) {
			t.Errorf("AmountOut = %s, want %s", got.AmountOut, want)
		}
		if want := dec("168"); !got.NetProfit.Equal(want) {
			t.Errorf("NetProfit = %s, want %s", got.NetProfit, want)
		}
		if want := dec("500"); !got.GrossProfit.Equal(want) {
			t.Errorf("GrossProfit = %s, want %s", got.GrossProfit, want)
		}
		if want := dec("332"); !got.Fees.Equal(want) {
			t.Errorf("Fees = %s, want %s", got.Fees, want)
		}
		if got.StartAsset != "USD" {
			t.Errorf("StartAsset = %q, want USD", got.StartAsset)
		}

		if len(got.Legs) != 2 {
			t.Fatalf("got %d legs, want 2", len(got.Legs))
		}
		buy, sell := got.Legs[0], got.Legs[1]
		if buy.Venue != "kraken" || buy.Side != opportunity.Buy {
			t.Errorf("leg 0 = %s %s, want kraken buy", buy.Venue, buy.Side)
		}
		if sell.Venue != "coinbase" || sell.Side != opportunity.Sell {
			t.Errorf("leg 1 = %s %s, want coinbase sell", sell.Venue, sell.Side)
		}
		if !buy.AmountOut.Equal(sell.AmountIn) {
			t.Errorf("legs do not chain: %s out, %s in", buy.AmountOut, sell.AmountIn)
		}
		if want := dec("50000"); !buy.Price.Equal(want) {
			t.Errorf("buy VWAP = %s, want %s", buy.Price, want)
		}
	})

	t.Run("skips a spread the fees eat", func(t *testing.T) {
		s := strategy()
		books := []market.Book{
			book("kraken", "BTC/USD", []market.Level{level("49900", "1")}, []market.Level{level("50000", "1")}),
			book("coinbase", "BTC/USD", []market.Level{level("50100", "1")}, []market.Level{level("50200", "1")}),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 0 {
			t.Fatalf("got %d opportunities, want none: gross spread is positive but 26+40bps of fees exceed it", len(opps))
		}
	})

	t.Run("skips an edge below the threshold", func(t *testing.T) {
		s := strategy()
		s.MinEdgeBps = decimal.NewFromInt(50)
		books := []market.Book{
			book("kraken", "BTC/USD", []market.Level{level("49900", "1")}, []market.Level{level("50000", "1")}),
			book("coinbase", "BTC/USD", []market.Level{level("50500", "1")}, []market.Level{level("50600", "1")}),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 0 {
			t.Fatalf("got %d opportunities, want none: 33bps of edge is below the 50bps floor", len(opps))
		}
	})

	t.Run("skips a notional below the minimum", func(t *testing.T) {
		s := freeStrategy()
		s.MinSizeQuote = decimal.NewFromInt(100)
		books := []market.Book{
			book("kraken", "BTC/USD", nil, []market.Level{level("50000", "0.001")}),
			book("coinbase", "BTC/USD", []market.Level{level("50500", "0.001")}, nil),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 0 {
			t.Fatalf("got %d opportunities, want none: 50 quote of depth is below the 100 floor", len(opps))
		}
	})

	t.Run("skips a stale book", func(t *testing.T) {
		s := strategy()
		stale := book("coinbase", "BTC/USD", []market.Level{level("50500", "1")}, nil)
		stale.UpdatedAt = evaluatedAt.Add(-time.Minute)

		opps, err := s.Generate([]market.Book{
			book("kraken", "BTC/USD", nil, []market.Level{level("50000", "1")}),
			stale,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 0 {
			t.Fatalf("got %d opportunities, want none from a book a minute old", len(opps))
		}
	})

	t.Run("skips a venue it has no fee for", func(t *testing.T) {
		s := strategy()
		opps, err := s.Generate([]market.Book{
			book("kraken", "BTC/USD", nil, []market.Level{level("50000", "1")}),
			book("binance", "BTC/USD", []market.Level{level("50500", "1")}, nil),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 0 {
			t.Fatalf("got %d opportunities, want none from an unconfigured venue", len(opps))
		}
	})

	t.Run("ranks the widest edge first", func(t *testing.T) {
		s := freeStrategy()
		books := []market.Book{
			book("kraken", "BTC/USD", nil, []market.Level{level("50000", "1")}),
			book("coinbase", "BTC/USD", []market.Level{level("50100", "1")}, nil),
			book("kraken", "ETH/USD", nil, []market.Level{level("2000", "10")}),
			book("coinbase", "ETH/USD", []market.Level{level("2100", "10")}, nil),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 2 {
			t.Fatalf("got %d opportunities, want 2", len(opps))
		}
		// ETH is 500bps wide against BTC's 20bps.
		if opps[0].Legs[0].Symbol != "ETH/USD" {
			t.Errorf("first opportunity is %s, want the wider ETH/USD", opps[0].Legs[0].Symbol)
		}
		if !opps[0].NetEdgeBps.GreaterThan(opps[1].NetEdgeBps) {
			t.Errorf("edges out of order: %s then %s", opps[0].NetEdgeBps, opps[1].NetEdgeBps)
		}
	})
}

func TestCrossVenueWalk(t *testing.T) {
	t.Run("stops where the edge closes", func(t *testing.T) {
		s := freeStrategy()
		books := []market.Book{
			book("kraken", "BTC/USD", nil, []market.Level{level("50000", "1"), level("50600", "5")}),
			book("coinbase", "BTC/USD", []market.Level{level("50500", "10")}, nil),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 1 {
			t.Fatalf("got %d opportunities, want 1", len(opps))
		}

		buy := opps[0].Legs[0]
		if want := dec("1"); !buy.Qty.Equal(want) {
			t.Errorf("Qty = %s, want %s: the 50600 ask is above the 50500 bid", buy.Qty, want)
		}
		if buy.LevelsConsumed != 1 {
			t.Errorf("LevelsConsumed = %d, want 1", buy.LevelsConsumed)
		}
	})

	t.Run("takes only what the thinner side offers", func(t *testing.T) {
		s := freeStrategy()
		books := []market.Book{
			book("kraken", "BTC/USD", nil, []market.Level{level("50000", "5")}),
			book("coinbase", "BTC/USD", []market.Level{level("50500", "0.2"), level("50400", "0.3")}, nil),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 1 {
			t.Fatalf("got %d opportunities, want 1", len(opps))
		}

		buy, sell := opps[0].Legs[0], opps[0].Legs[1]
		if want := dec("0.5"); !buy.Qty.Equal(want) {
			t.Errorf("Qty = %s, want %s: the ask level is only partly consumed", buy.Qty, want)
		}
		if buy.LevelsConsumed != 1 || sell.LevelsConsumed != 2 {
			t.Errorf("levels consumed = %d buy, %d sell, want 1 and 2", buy.LevelsConsumed, sell.LevelsConsumed)
		}
		// 0.2 at 50500 plus 0.3 at 50400 over 0.5 base.
		if want := dec("50440"); !sell.Price.Equal(want) {
			t.Errorf("sell VWAP = %s, want %s", sell.Price, want)
		}
	})

	t.Run("finds the route in either direction", func(t *testing.T) {
		s := freeStrategy()
		books := []market.Book{
			book("kraken", "BTC/USD", []market.Level{level("50500", "1")}, nil),
			book("coinbase", "BTC/USD", nil, []market.Level{level("50000", "1")}),
		}

		opps, err := s.Generate(books)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opps) != 1 {
			t.Fatalf("got %d opportunities, want 1", len(opps))
		}
		if opps[0].Legs[0].Venue != "coinbase" || opps[0].Legs[1].Venue != "kraken" {
			t.Errorf("route = buy %s sell %s, want buy coinbase sell kraken", opps[0].Legs[0].Venue, opps[0].Legs[1].Venue)
		}
	})
}
