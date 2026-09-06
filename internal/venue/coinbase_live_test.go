//go:build live

package venue

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestCoinbaseLive connects to the real feed. Excluded from the default build:
//
//	./scripts/test.sh -tags=live -run TestCoinbaseLive -v
//
// LIVE_DURATION controls how long it watches (default 20s).
func TestCoinbaseLive(t *testing.T) {
	duration := 20 * time.Second
	if v := os.Getenv("LIVE_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("LIVE_DURATION: %v", err)
		}
		duration = d
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	feed := NewCoinbase(log, 10, "BTC/USD", "ETH/USD")

	go func() {
		if err := feed.Run(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("feed stopped: %v", err)
		}
	}()

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	priced := 0
	for {
		select {
		case <-ctx.Done():
			if priced == 0 {
				t.Fatal("no priced book received from the live feed")
			}
			return

		case <-tick.C:
			for _, book := range feed.Books() {
				bid, hasBid := book.BestBid()
				ask, hasAsk := book.BestAsk()
				if !hasBid || !hasAsk {
					t.Logf("%-8s %-8s waiting for snapshot", book.Venue, book.Symbol)
					continue
				}
				priced++

				spread := ask.Price.Sub(bid.Price)
				t.Logf("%-8s %-8s bid %s x %s | ask %s x %s | spread %s | levels %d/%d | age %v",
					book.Venue, book.Symbol,
					bid.Price, bid.Size, ask.Price, ask.Size, spread,
					len(book.Bids), len(book.Asks),
					time.Since(book.UpdatedAt).Truncate(time.Millisecond),
				)
			}
		}
	}
}
