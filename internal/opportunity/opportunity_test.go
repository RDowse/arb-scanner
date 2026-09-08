package opportunity

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func crossVenueLegs() []Leg {
	return []Leg{
		{
			Venue: "kraken", Symbol: "BTC/USD", Side: Buy,
			Base: "BTC", Quote: "USD", AssetIn: "USD", AssetOut: "BTC",
			AmountIn: dec("10000"), AmountOut: dec("0.2"), Price: dec("50000"), Qty: dec("0.2"),
		},
		{
			Venue: "coinbase", Symbol: "BTC/USD", Side: Sell,
			Base: "BTC", Quote: "USD", AssetIn: "BTC", AssetOut: "USD",
			AmountIn: dec("0.2"), AmountOut: dec("10100"), Price: dec("50500"), Qty: dec("0.2"),
		},
	}
}

func TestNew(t *testing.T) {
	observedAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	t.Run("derives profit from the route", func(t *testing.T) {
		got, err := New("cross_venue", "v1", observedAt, dec("10000"), dec("10100"), dec("66"), crossVenueLegs())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StartAsset != "USD" {
			t.Errorf("StartAsset = %q, want USD", got.StartAsset)
		}
		if want := dec("100"); !got.NetProfit.Equal(want) {
			t.Errorf("NetProfit = %s, want %s", got.NetProfit, want)
		}
		if want := dec("166"); !got.GrossProfit.Equal(want) {
			t.Errorf("GrossProfit = %s, want %s", got.GrossProfit, want)
		}
		if want := dec("100"); !got.NetEdgeBps.Equal(want) {
			t.Errorf("NetEdgeBps = %s, want %s", got.NetEdgeBps, want)
		}
		if got.RouteID == "" {
			t.Error("RouteID is empty")
		}
		if got.ID == "" {
			t.Error("ID is empty")
		}
	})

	t.Run("rejects a broken chain", func(t *testing.T) {
		legs := crossVenueLegs()
		legs[1].AssetIn = "ETH"

		_, err := New("cross_venue", "v1", observedAt, dec("10000"), dec("10100"), dec("66"), legs)
		if err == nil {
			t.Fatal("expected an error for a leg that does not follow the previous one")
		}
		if !strings.Contains(err.Error(), "leg 1 takes ETH") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("rejects a route that does not return to its start asset", func(t *testing.T) {
		legs := crossVenueLegs()
		legs[1].AssetOut = "EUR"

		if _, err := New("cross_venue", "v1", observedAt, dec("10000"), dec("10100"), dec("66"), legs); err == nil {
			t.Fatal("expected an error for a route ending in another asset")
		}
	})

	t.Run("rejects an empty route", func(t *testing.T) {
		if _, err := New("cross_venue", "v1", observedAt, dec("10000"), dec("10100"), dec("66"), nil); err == nil {
			t.Fatal("expected an error for a route with no legs")
		}
	})
}

func TestRouteID(t *testing.T) {
	first, err := New("cross_venue", "v1", time.Now(), dec("10000"), dec("10100"), dec("66"), crossVenueLegs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("ignores prices and timing", func(t *testing.T) {
		legs := crossVenueLegs()
		legs[1].Price = dec("50900")
		legs[1].AmountOut = dec("10180")

		later, err := New("cross_venue", "v1", time.Now().Add(time.Minute), dec("10000"), dec("10180"), dec("66"), legs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if later.RouteID != first.RouteID {
			t.Errorf("RouteID = %q, want %q for the same route", later.RouteID, first.RouteID)
		}
		if later.ID == first.ID {
			t.Error("two sightings of one route share a sighting id")
		}
	})

	t.Run("separates a reversed route", func(t *testing.T) {
		legs := crossVenueLegs()
		legs[0].Venue, legs[1].Venue = legs[1].Venue, legs[0].Venue

		reversed, err := New("cross_venue", "v1", time.Now(), dec("10000"), dec("10100"), dec("66"), legs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reversed.RouteID == first.RouteID {
			t.Error("routes buying and selling on swapped venues share a RouteID")
		}
	})
}
