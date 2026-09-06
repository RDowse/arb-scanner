package venue

import (
	"io"
	"log/slog"
	"testing"
)

func testCoinbase(t *testing.T, depth int) *Coinbase {
	t.Helper()
	return NewCoinbase(slog.New(slog.NewTextHandler(io.Discard, nil)), depth, "BTC/USD")
}

func TestCoinbaseSnapshot(t *testing.T) {
	c := testCoinbase(t, 5)

	err := c.handle([]byte(`{"type":"snapshot","product_id":"BTC-USD",
		"bids":[["99.00","2"],["100.00","1"]],
		"asks":[["102.00","3"],["101.00","1"]]}`))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	book := c.Books()[0]
	if book.Symbol != "BTC/USD" || book.Venue != CoinbaseName {
		t.Fatalf("unexpected identity: %+v", book)
	}

	bid, ok := book.BestBid()
	if !ok || bid.Price.String() != "100" {
		t.Errorf("best bid = %v (ok=%v), want 100", bid.Price, ok)
	}
	ask, ok := book.BestAsk()
	if !ok || ask.Price.String() != "101" {
		t.Errorf("best ask = %v (ok=%v), want 101", ask.Price, ok)
	}
	if book.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not stamped")
	}
}

func TestCoinbaseUpdateRemovesAndAddsLevels(t *testing.T) {
	c := testCoinbase(t, 5)

	if err := c.handle([]byte(`{"type":"snapshot","product_id":"BTC-USD",
		"bids":[["100.00","1"],["99.00","2"]],
		"asks":[["101.00","1"]]}`)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Zero size deletes the level; a new price inserts one.
	if err := c.handle([]byte(`{"type":"l2update","product_id":"BTC-USD",
		"time":"2026-09-05T10:00:00Z",
		"changes":[["buy","100.00","0"],["sell","100.50","4"]]}`)); err != nil {
		t.Fatalf("update: %v", err)
	}

	book := c.Books()[0]
	if bid, _ := book.BestBid(); bid.Price.String() != "99" {
		t.Errorf("best bid = %v, want 99 after deletion", bid.Price)
	}
	ask, _ := book.BestAsk()
	if ask.Price.String() != "100.5" || ask.Size.String() != "4" {
		t.Errorf("best ask = %v x %v, want 100.5 x 4", ask.Price, ask.Size)
	}
	if got := book.UpdatedAt.Format("2006-01-02T15:04:05Z"); got != "2026-09-05T10:00:00Z" {
		t.Errorf("UpdatedAt = %v, want the message timestamp", got)
	}
}

func TestCoinbaseDepthLimit(t *testing.T) {
	c := testCoinbase(t, 2)

	if err := c.handle([]byte(`{"type":"snapshot","product_id":"BTC-USD",
		"bids":[["100","1"],["99","1"],["98","1"],["97","1"]],
		"asks":[["101","1"],["102","1"],["103","1"]]}`)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	book := c.Books()[0]
	if len(book.Bids) != 2 || len(book.Asks) != 2 {
		t.Fatalf("depth not applied: %d bids, %d asks", len(book.Bids), len(book.Asks))
	}
	if book.Bids[0].Price.String() != "100" || book.Bids[1].Price.String() != "99" {
		t.Errorf("bids not sorted descending: %v", book.Bids)
	}
	if book.Asks[0].Price.String() != "101" || book.Asks[1].Price.String() != "102" {
		t.Errorf("asks not sorted ascending: %v", book.Asks)
	}
}

func TestCoinbaseHandleErrors(t *testing.T) {
	c := testCoinbase(t, 5)

	if err := c.handle([]byte(`{"type":"error","message":"bad","reason":"unknown product"}`)); err == nil {
		t.Error("expected error message to surface as an error")
	}
	if err := c.handle([]byte(`{"type":"heartbeat","product_id":"BTC-USD"}`)); err != nil {
		t.Errorf("heartbeat should be ignored: %v", err)
	}
	if err := c.handle([]byte(`{"type":"snapshot","product_id":"BTC-USD","bids":[["oops","1"]]}`)); err == nil {
		t.Error("expected unparseable price to error")
	}
	if err := c.handle([]byte(`{"type":"l2update","product_id":"ETH-USD","changes":[["buy","1","1"]]}`)); err != nil {
		t.Errorf("unsubscribed product should be ignored: %v", err)
	}
}

func TestCoinbaseInvalidate(t *testing.T) {
	c := testCoinbase(t, 5)

	if err := c.handle([]byte(`{"type":"snapshot","product_id":"BTC-USD",
		"bids":[["100","1"]],"asks":[["101","1"]]}`)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	c.invalidate()

	book := c.Books()[0]
	if len(book.Bids) != 0 || len(book.Asks) != 0 || !book.UpdatedAt.IsZero() {
		t.Errorf("book survived invalidation: %+v", book)
	}
}
