package strategy

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
)

var _ Strategy = (*Triangular)(nil)

// Triangular walks three symbols within a single venue back to the asset it
// started in.
type Triangular struct {
	ConfigID string

	// Books from other venues are ignored.
	Venue    string
	TakerFee decimal.Decimal

	MinEdgeBps   decimal.Decimal
	MinSizeQuote decimal.Decimal
	MaxBookAge   time.Duration

	Now func() time.Time
}

func (s *Triangular) Name() string { return "triangular" }

func (s *Triangular) Generate(books []market.Book) ([]opportunity.Opportunity, error) {
	// TODO: find the cycles this venue's symbols close, then walk each direction.
	return nil, nil
}

func (s *Triangular) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
