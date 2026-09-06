package venue

import (
	"testing"

	"github.com/shopspring/decimal"
)

func dec(t *testing.T, s string) decimal.Decimal {
	t.Helper()

	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal %q: %v", s, err)
	}
	return d
}

func TestBookStateTrim(t *testing.T) {
	build := func(t *testing.T) *bookState {
		t.Helper()

		s := newBookState()
		for _, price := range []string{"100", "99", "98", "97", "96"} {
			if err := s.set(bid, price, "1"); err != nil {
				t.Fatalf("set bid %s: %v", price, err)
			}
		}
		for _, price := range []string{"101", "102", "103", "104", "105"} {
			if err := s.set(ask, price, "1"); err != nil {
				t.Fatalf("set ask %s: %v", price, err)
			}
		}
		return s
	}

	t.Run("keeps the best levels on each side", func(t *testing.T) {
		s := build(t)
		s.trim(2)

		book := s.book("kraken", "BTC/USD", 0)
		if len(book.Bids) != 2 || len(book.Asks) != 2 {
			t.Fatalf("got %d bids and %d asks, want 2 of each", len(book.Bids), len(book.Asks))
		}
		if book.Bids[0].Price.String() != "100" || book.Bids[1].Price.String() != "99" {
			t.Errorf("bids = %v, want the two highest", book.Bids)
		}
		if book.Asks[0].Price.String() != "101" || book.Asks[1].Price.String() != "102" {
			t.Errorf("asks = %v, want the two lowest", book.Asks)
		}
	})

	t.Run("leaves a shallower book alone", func(t *testing.T) {
		s := build(t)
		s.trim(50)

		if book := s.book("kraken", "BTC/USD", 0); len(book.Bids) != 5 {
			t.Errorf("got %d bids, want all 5", len(book.Bids))
		}
	})

	t.Run("ignores a non-positive depth", func(t *testing.T) {
		s := build(t)
		s.trim(0)

		if book := s.book("kraken", "BTC/USD", 0); len(book.Bids) != 5 {
			t.Errorf("got %d bids, want all 5 when depth is unset", len(book.Bids))
		}
	})
}
