package strategy

import (
	"sort"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
)

// Strategy reads one tick's books, which it must not retain or mutate, and
// returns opportunities ranked best first with each route's legs in execution
// order.
type Strategy interface {
	Name() string
	Generate(books []market.Book) ([]opportunity.Opportunity, error)
}

// Rank orders by widest net edge, with profit and route id breaking ties so
// equal edges stay stable across ticks.
func Rank(opps []opportunity.Opportunity) {
	sort.SliceStable(opps, func(i, j int) bool {
		a, b := opps[i], opps[j]
		if !a.NetEdgeBps.Equal(b.NetEdgeBps) {
			return a.NetEdgeBps.GreaterThan(b.NetEdgeBps)
		}
		if !a.NetProfit.Equal(b.NetProfit) {
			return a.NetProfit.GreaterThan(b.NetProfit)
		}
		return a.RouteID < b.RouteID
	})
}
