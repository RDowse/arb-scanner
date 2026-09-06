package strategy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
)

var one = decimal.NewFromInt(1)

var _ Strategy = (*CrossVenue)(nil)

// CrossVenue buys the base at one venue's ask and sells it at another's bid,
// starting and ending in the quote asset.
type CrossVenue struct {
	ConfigID string

	// Rates, not bps. A venue absent from the map is not traded.
	TakerFees map[string]decimal.Decimal

	MinEdgeBps   decimal.Decimal
	MinSizeQuote decimal.Decimal

	// Zero disables the staleness guard.
	MaxBookAge time.Duration

	Now func() time.Time
}

func (s *CrossVenue) Name() string { return "cross_venue" }

func (s *CrossVenue) Generate(books []market.Book) ([]opportunity.Opportunity, error) {
	now := s.now()
	bySymbol := s.tradable(books, now)

	symbols := make([]string, 0, len(bySymbol))
	for symbol := range bySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	var opps []opportunity.Opportunity
	for _, symbol := range symbols {
		base, quote, ok := splitSymbol(symbol)
		if !ok {
			continue
		}

		group := bySymbol[symbol]
		for _, buy := range group {
			for _, sell := range group {
				if buy.Venue == sell.Venue {
					continue
				}

				opp, found, err := s.evaluate(base, quote, buy, sell, now)
				if err != nil {
					return nil, err
				}
				if found {
					opps = append(opps, opp)
				}
			}
		}
	}

	Rank(opps)
	return opps, nil
}

// tradable groups the books this strategy may trade by symbol, dropping venues
// it has no fee for and books too old to price against.
func (s *CrossVenue) tradable(books []market.Book, now time.Time) map[string][]market.Book {
	bySymbol := make(map[string][]market.Book)
	for _, b := range books {
		if _, ok := s.TakerFees[b.Venue]; !ok {
			continue
		}
		if s.MaxBookAge > 0 && b.StaleAt(now, s.MaxBookAge) {
			continue
		}
		bySymbol[b.Symbol] = append(bySymbol[b.Symbol], b)
	}

	for symbol, group := range bySymbol {
		sort.Slice(group, func(i, j int) bool { return group[i].Venue < group[j].Venue })
		bySymbol[symbol] = group
	}
	return bySymbol
}

func (s *CrossVenue) evaluate(base, quote string, buy, sell market.Book, now time.Time) (opportunity.Opportunity, bool, error) {
	buyFee := s.TakerFees[buy.Venue]
	sellFee := s.TakerFees[sell.Venue]

	w := walk(buy.Asks, sell.Bids, buyFee, sellFee)
	if w.qty.IsZero() {
		return opportunity.Opportunity{}, false, nil
	}

	buyFeeAmount := w.cost.Mul(buyFee)
	sellFeeAmount := w.proceeds.Mul(sellFee)
	amountIn := w.cost.Add(buyFeeAmount)
	amountOut := w.proceeds.Sub(sellFeeAmount)

	if amountIn.LessThanOrEqual(s.MinSizeQuote) {
		return opportunity.Opportunity{}, false, nil
	}

	legs := []opportunity.Leg{
		{
			Venue:          buy.Venue,
			Symbol:         buy.Symbol,
			Side:           opportunity.Buy,
			Base:           base,
			Quote:          quote,
			AssetIn:        quote,
			AssetOut:       base,
			AmountIn:       amountIn,
			AmountOut:      w.qty,
			Price:          w.cost.Div(w.qty),
			Qty:            w.qty,
			Fee:            buyFeeAmount,
			LevelsConsumed: w.askLevels,
			BookAt:         buy.UpdatedAt,
		},
		{
			Venue:          sell.Venue,
			Symbol:         sell.Symbol,
			Side:           opportunity.Sell,
			Base:           base,
			Quote:          quote,
			AssetIn:        base,
			AssetOut:       quote,
			AmountIn:       w.qty,
			AmountOut:      amountOut,
			Price:          w.proceeds.Div(w.qty),
			Qty:            w.qty,
			Fee:            sellFeeAmount,
			LevelsConsumed: w.bidLevels,
			BookAt:         sell.UpdatedAt,
		},
	}

	opp, err := opportunity.New(s.Name(), s.ConfigID, now, amountIn, amountOut, buyFeeAmount.Add(sellFeeAmount), legs)
	if err != nil {
		return opportunity.Opportunity{}, false, fmt.Errorf("%s %s->%s: %w", buy.Symbol, buy.Venue, sell.Venue, err)
	}
	if opp.NetEdgeBps.LessThanOrEqual(s.MinEdgeBps) {
		return opportunity.Opportunity{}, false, nil
	}
	return opp, true, nil
}

type walkResult struct {
	qty       decimal.Decimal
	cost      decimal.Decimal
	proceeds  decimal.Decimal
	askLevels int
	bidLevels int
}

// walk consumes asks against bids while the pair still clears both fees,
// advancing whichever side exhausts first, so the last level of either side is
// usually taken in part.
func walk(asks, bids []market.Level, buyFee, sellFee decimal.Decimal) walkResult {
	w := walkResult{qty: decimal.Zero, cost: decimal.Zero, proceeds: decimal.Zero}
	if len(asks) == 0 || len(bids) == 0 {
		return w
	}

	buyRate := one.Add(buyFee)
	sellRate := one.Sub(sellFee)

	ai, bi := 0, 0
	askLeft, bidLeft := asks[0].Size, bids[0].Size
	for ai < len(asks) && bi < len(bids) {
		ask, bid := asks[ai].Price, bids[bi].Price
		if bid.Mul(sellRate).LessThanOrEqual(ask.Mul(buyRate)) {
			break
		}

		take := decimal.Min(askLeft, bidLeft)
		if !take.IsPositive() {
			break
		}

		w.qty = w.qty.Add(take)
		w.cost = w.cost.Add(ask.Mul(take))
		w.proceeds = w.proceeds.Add(bid.Mul(take))
		w.askLevels, w.bidLevels = ai+1, bi+1

		askLeft = askLeft.Sub(take)
		bidLeft = bidLeft.Sub(take)
		if askLeft.IsZero() {
			if ai++; ai < len(asks) {
				askLeft = asks[ai].Size
			}
		}
		if bidLeft.IsZero() {
			if bi++; bi < len(bids) {
				bidLeft = bids[bi].Size
			}
		}
	}
	return w
}

func splitSymbol(symbol string) (base, quote string, ok bool) {
	base, quote, ok = strings.Cut(symbol, "/")
	return base, quote, ok && base != "" && quote != ""
}

func (s *CrossVenue) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
