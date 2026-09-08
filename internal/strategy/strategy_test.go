package strategy

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

func TestRank(t *testing.T) {
	opps := []opportunity.Opportunity{
		{RouteID: "c", NetEdgeBps: decimal.NewFromInt(5), NetProfit: decimal.NewFromInt(50)},
		{RouteID: "b", NetEdgeBps: decimal.NewFromInt(20), NetProfit: decimal.NewFromInt(10)},
		{RouteID: "a", NetEdgeBps: decimal.NewFromInt(5), NetProfit: decimal.NewFromInt(50)},
		{RouteID: "d", NetEdgeBps: decimal.NewFromInt(5), NetProfit: decimal.NewFromInt(80)},
	}

	Rank(opps)

	want := []string{"b", "d", "a", "c"}
	for i, id := range want {
		if opps[i].RouteID != id {
			t.Fatalf("order = %s, want %s", ids(opps), want)
		}
	}
}

func ids(opps []opportunity.Opportunity) []string {
	out := make([]string, 0, len(opps))
	for _, o := range opps {
		out = append(out, o.RouteID)
	}
	return out
}
