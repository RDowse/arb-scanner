package venue

import (
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/market"
)

type side int

const (
	bid side = iota
	ask
)

// bookState keeps price levels in maps because venue updates address levels by
// price, not by index. Sorting happens when a Book is read, which is once per
// evaluation tick rather than on every update.
type bookState struct {
	bids      map[string]decimal.Decimal
	asks      map[string]decimal.Decimal
	updatedAt time.Time
}

func newBookState() *bookState {
	return &bookState{
		bids: make(map[string]decimal.Decimal),
		asks: make(map[string]decimal.Decimal),
	}
}

func (b *bookState) reset() {
	b.bids = make(map[string]decimal.Decimal)
	b.asks = make(map[string]decimal.Decimal)
	b.updatedAt = time.Time{}
}

func (b *bookState) set(s side, price, size string) error {
	p, err := decimal.NewFromString(price)
	if err != nil {
		return fmt.Errorf("price %q: %w", price, err)
	}
	q, err := decimal.NewFromString(size)
	if err != nil {
		return fmt.Errorf("size %q: %w", size, err)
	}

	levels := b.bids
	if s == ask {
		levels = b.asks
	}

	// A zero size means the level is gone, not that it holds nothing.
	if q.IsZero() {
		delete(levels, p.String())
		return nil
	}
	levels[p.String()] = q
	return nil
}

func (b *bookState) book(venue, symbol string, depth int) market.Book {
	return market.Book{
		Venue:     venue,
		Symbol:    symbol,
		Bids:      sortedLevels(b.bids, true, depth),
		Asks:      sortedLevels(b.asks, false, depth),
		UpdatedAt: b.updatedAt,
	}
}

func sortedLevels(levels map[string]decimal.Decimal, descending bool, depth int) []market.Level {
	out := make([]market.Level, 0, len(levels))
	for price, size := range levels {
		p, err := decimal.NewFromString(price)
		if err != nil {
			continue
		}
		out = append(out, market.Level{Price: p, Size: size})
	}

	sort.Slice(out, func(i, j int) bool {
		if descending {
			return out[i].Price.GreaterThan(out[j].Price)
		}
		return out[i].Price.LessThan(out[j].Price)
	})

	if depth > 0 && len(out) > depth {
		out = out[:depth]
	}
	return out
}
