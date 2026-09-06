package market

import (
	"time"

	"github.com/shopspring/decimal"
)

type Level struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}

// Book holds one venue's aggregated depth for one symbol. Bids descend by
// price, asks ascend, so index 0 of each is the top of book.
type Book struct {
	Venue     string
	Symbol    string
	Bids      []Level
	Asks      []Level
	UpdatedAt time.Time
}

func (b Book) BestBid() (Level, bool) {
	if len(b.Bids) == 0 {
		return Level{}, false
	}
	return b.Bids[0], true
}

func (b Book) BestAsk() (Level, bool) {
	if len(b.Asks) == 0 {
		return Level{}, false
	}
	return b.Asks[0], true
}

func (b Book) StaleAt(now time.Time, maxAge time.Duration) bool {
	return b.UpdatedAt.IsZero() || now.Sub(b.UpdatedAt) > maxAge
}
