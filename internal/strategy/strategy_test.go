package strategy

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

func TestRank(t *testing.T) {
	opps := []opportunity.Opportunity{
		{ID: "c", NetEdgeBps: decimal.NewFromInt(5), NetProfit: decimal.NewFromInt(50)},
		{ID: "b", NetEdgeBps: decimal.NewFromInt(20), NetProfit: decimal.NewFromInt(10)},
		{ID: "a", NetEdgeBps: decimal.NewFromInt(5), NetProfit: decimal.NewFromInt(50)},
		{ID: "d", NetEdgeBps: decimal.NewFromInt(5), NetProfit: decimal.NewFromInt(80)},
	}

	Rank(opps)

	want := []string{"b", "d", "a", "c"}
	for i, id := range want {
		if opps[i].ID != id {
			t.Fatalf("order = %s, want %s", ids(opps), want)
		}
	}
}

func ids(opps []opportunity.Opportunity) []string {
	out := make([]string, 0, len(opps))
	for _, o := range opps {
		out = append(out, o.ID)
	}
	return out
}
