package venue

import (
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func testKraken(t *testing.T, depth int, symbols ...string) *Kraken {
	t.Helper()
	if len(symbols) == 0 {
		symbols = []string{"BTC/USD"}
	}
	return NewKraken(slog.New(slog.NewTextHandler(io.Discard, nil)), depth, symbols...)
}

func TestKrakenSnapshot(t *testing.T) {
	k := testKraken(t, 5)

	err := k.handle([]byte(`{"channel":"book","type":"snapshot","data":[{
		"symbol":"BTC/USD",
		"bids":[{"price":99.0,"qty":2.0},{"price":100.0,"qty":1.0}],
		"asks":[{"price":102.0,"qty":3.0},{"price":101.0,"qty":1.0}]
	}]}`))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	book := k.Books()[0]
	if book.Symbol != "BTC/USD" || book.Venue != KrakenName {
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

func TestKrakenUpdateRemovesAndAddsLevels(t *testing.T) {
	k := testKraken(t, 5)

	if err := k.handle([]byte(`{"channel":"book","type":"snapshot","data":[{
		"symbol":"BTC/USD",
		"bids":[{"price":100.0,"qty":1.0},{"price":99.0,"qty":2.0}],
		"asks":[{"price":101.0,"qty":1.0}]
	}]}`)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// An update edits in place; qty 0 removes the level.
	if err := k.handle([]byte(`{"channel":"book","type":"update","data":[{
		"symbol":"BTC/USD",
		"timestamp":"2026-09-05T10:00:00Z",
		"bids":[{"price":100.0,"qty":0}],
		"asks":[{"price":100.5,"qty":4.0}]
	}]}`)); err != nil {
		t.Fatalf("update: %v", err)
	}

	book := k.Books()[0]
	if bid, _ := book.BestBid(); bid.Price.String() != "99" {
		t.Errorf("best bid = %v, want 99 after deletion", bid.Price)
	}
	if ask, _ := book.BestAsk(); ask.Price.String() != "100.5" {
		t.Errorf("best ask = %v, want 100.5 after insertion", ask.Price)
	}
	if !book.UpdatedAt.Equal(book.UpdatedAt.UTC()) || book.UpdatedAt.Year() != 2026 {
		t.Errorf("UpdatedAt = %s, want the message timestamp", book.UpdatedAt)
	}
}

// A depth-limited feed never deletes a level its own window pushed out, so the
// book must drop it locally or a stale price resurfaces as best-of-book.
func TestKrakenTrimsBeyondSubscribedDepth(t *testing.T) {
	k := testKraken(t, 10)
	if k.feedDepth != 10 {
		t.Fatalf("feedDepth = %d, want 10", k.feedDepth)
	}

	var levels []string
	for i := 0; i < 10; i++ {
		levels = append(levels, fmt.Sprintf(`{"price":%d.0,"qty":1.0}`, 100-i))
	}
	if err := k.handle([]byte(`{"channel":"book","type":"snapshot","data":[{
		"symbol":"BTC/USD","bids":[` + strings.Join(levels, ",") + `],"asks":[]}]}`)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Ten better bids arrive, pushing the originals out of Kraken's window
	// without any delete being sent for them.
	var better []string
	for i := 0; i < 10; i++ {
		better = append(better, fmt.Sprintf(`{"price":%d.0,"qty":1.0}`, 110-i))
	}
	if err := k.handle([]byte(`{"channel":"book","type":"update","data":[{
		"symbol":"BTC/USD","bids":[` + strings.Join(better, ",") + `],"asks":[]}]}`)); err != nil {
		t.Fatalf("update: %v", err)
	}

	book := k.Books()[0]
	if len(book.Bids) != 10 {
		t.Fatalf("got %d bids, want 10 after trimming", len(book.Bids))
	}
	if worst := book.Bids[len(book.Bids)-1]; worst.Price.String() != "101" {
		t.Errorf("worst retained bid = %s, want 101: stale levels were kept", worst.Price)
	}
}

func TestKrakenChecksum(t *testing.T) {
	k := testKraken(t, 10)

	if err := k.handle([]byte(`{"channel":"instrument","type":"snapshot","data":{
		"pairs":[{"symbol":"BTC/USD","price_precision":1,"qty_precision":8}]
	}}`)); err != nil {
		t.Fatalf("instrument: %v", err)
	}

	// Precision restores the digits the JSON number dropped: 100.0 hashes as
	// "1000" at one decimal place, and qty 1.5 as "150000000" at eight.
	want := crc32.ChecksumIEEE([]byte("1010" + "150000000" + "1000" + "250000000"))

	body := fmt.Sprintf(`{"channel":"book","type":"snapshot","data":[{
		"symbol":"BTC/USD",
		"bids":[{"price":100.0,"qty":2.5}],
		"asks":[{"price":101.0,"qty":1.5}],
		"checksum":%d
	}]}`, want)

	if err := k.handle([]byte(body)); err != nil {
		t.Fatalf("handle: %v", err)
	}

	t.Run("rejects a book that has diverged", func(t *testing.T) {
		err := k.handle([]byte(`{"channel":"book","type":"update","data":[{
			"symbol":"BTC/USD",
			"bids":[{"price":100.0,"qty":9.0}],
			"asks":[],
			"checksum":12345
		}]}`))
		if err == nil {
			t.Fatal("expected a checksum error so the caller reconnects")
		}
		if !strings.Contains(err.Error(), "checksum") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestKrakenChecksumToken(t *testing.T) {
	tests := []struct {
		value  string
		places int32
		want   string
	}{
		{"100.0", 1, "1000"},
		{"0.5", 8, "50000000"},
		{"0.0", 8, "0"},
		{"50000.10", 1, "500001"},
		{"1.5", 8, "150000000"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := krakenChecksumToken(dec(t, tt.value), tt.places); got != tt.want {
				t.Errorf("token(%s, %d) = %q, want %q", tt.value, tt.places, got, tt.want)
			}
		})
	}
}

func TestKrakenSkipsChecksumBeforeInstruments(t *testing.T) {
	k := testKraken(t, 10)

	// No instrument snapshot yet, so precision is unknown and a checksum
	// cannot be reproduced; the book must still be accepted.
	err := k.handle([]byte(`{"channel":"book","type":"snapshot","data":[{
		"symbol":"BTC/USD",
		"bids":[{"price":100.0,"qty":1.0}],
		"asks":[{"price":101.0,"qty":1.0}],
		"checksum":999
	}]}`))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if bid, ok := k.Books()[0].BestBid(); !ok || bid.Price.String() != "100" {
		t.Errorf("best bid = %v, want the book applied", bid.Price)
	}
}

func TestKrakenHandleErrors(t *testing.T) {
	k := testKraken(t, 5)

	t.Run("rejected subscription", func(t *testing.T) {
		err := k.handle([]byte(`{"method":"subscribe","success":false,"error":"Currency pair not supported"}`))
		if err == nil {
			t.Fatal("expected an error for a rejected subscription")
		}
	})

	t.Run("accepted subscription", func(t *testing.T) {
		if err := k.handle([]byte(`{"method":"subscribe","success":true}`)); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("heartbeat and status are ignored", func(t *testing.T) {
		for _, frame := range []string{`{"channel":"heartbeat"}`, `{"channel":"status","data":[{"version":"2.0"}]}`} {
			if err := k.handle([]byte(frame)); err != nil {
				t.Errorf("%s: %v", frame, err)
			}
		}
	})

	t.Run("untracked symbol is ignored", func(t *testing.T) {
		err := k.handle([]byte(`{"channel":"book","type":"snapshot","data":[{
			"symbol":"DOGE/USD","bids":[{"price":1.0,"qty":1.0}],"asks":[]}]}`))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		if err := k.handle([]byte(`{"channel":`)); err == nil {
			t.Error("expected a decode error")
		}
	})
}

func TestKrakenInvalidate(t *testing.T) {
	k := testKraken(t, 5)

	if err := k.handle([]byte(`{"channel":"book","type":"snapshot","data":[{
		"symbol":"BTC/USD","bids":[{"price":100.0,"qty":1.0}],"asks":[{"price":101.0,"qty":1.0}]}]}`)); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	k.invalidate()

	book := k.Books()[0]
	if _, ok := book.BestBid(); ok {
		t.Error("bids survived invalidation")
	}
	if !book.UpdatedAt.IsZero() {
		t.Error("UpdatedAt survived invalidation")
	}
}

func TestKrakenFeedDepth(t *testing.T) {
	tests := []struct {
		asked int
		want  int
	}{
		{5, 10},
		{10, 10},
		{20, 25},
		{25, 25},
		{200, 500},
		{5000, 1000},
	}
	for _, tt := range tests {
		if got := krakenFeedDepth(tt.asked); got != tt.want {
			t.Errorf("krakenFeedDepth(%d) = %d, want %d", tt.asked, got, tt.want)
		}
	}
}
