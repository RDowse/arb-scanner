package strategy

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
)

var _ Strategy = (*CrossVenue)(nil)

// CrossVenue buys the base at one venue's ask and sells it at another's bid,
// starting and ending in the quote asset.
type CrossVenue struct {
	ConfigID string

	// Rates, not bps. A venue absent from the map is not traded.
	TakerFees map[string]decimal.Decimal

	MinEdgeBps   decimal.Decimal
	MinSizeQuote decimal.Decimal
	MaxBookAge   time.Duration

	Now func() time.Time
}

func (s *CrossVenue) Name() string { return "cross_venue" }

func (s *CrossVenue) Generate(books []market.Book) ([]opportunity.Opportunity, error) {
	// TODO: walk each venue pair's asks against bids until the edge closes.
	return nil, nil
}

func (s *CrossVenue) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
