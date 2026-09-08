package opportunity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

var bps = decimal.NewFromInt(10_000)

// Leg is one venue-side trade in a route. AssetIn and AssetOut are what the leg
// consumes and produces, so a route can be checked for chain validity without
// re-deriving direction from Side.
type Leg struct {
	Venue     string          `json:"venue"`
	Symbol    string          `json:"symbol"`
	Side      Side            `json:"side"`
	Base      string          `json:"base"`
	Quote     string          `json:"quote"`
	AssetIn   string          `json:"asset_in"`
	AssetOut  string          `json:"asset_out"`
	AmountIn  decimal.Decimal `json:"amount_in"`
	AmountOut decimal.Decimal `json:"amount_out"`
	// VWAP across the consumed levels.
	Price decimal.Decimal `json:"price"`
	// Base amount filled.
	Qty            decimal.Decimal `json:"qty"`
	Fee            decimal.Decimal `json:"fee"`
	LevelsConsumed int             `json:"levels_consumed"`
	BookAt         time.Time       `json:"book_at"`
}

// Opportunity is one sighting of an ordered route back to its starting asset.
// Legs are executed in slice order; every derived amount is quoted in
// StartAsset. ID identifies the sighting, RouteID the route it is a sighting
// of, so a route's history is the rows sharing a RouteID.
type Opportunity struct {
	ID          string          `json:"id"`
	RouteID     string          `json:"route_id"`
	Strategy    string          `json:"strategy"`
	ConfigID    string          `json:"config_id"`
	ObservedAt  time.Time       `json:"observed_at"`
	StartAsset  string          `json:"start_asset"`
	AmountIn    decimal.Decimal `json:"amount_in"`
	AmountOut   decimal.Decimal `json:"amount_out"`
	GrossProfit decimal.Decimal `json:"gross_profit"`
	Fees        decimal.Decimal `json:"fees"`
	NetProfit   decimal.Decimal `json:"net_profit"`
	NetEdgeBps  decimal.Decimal `json:"net_edge_bps"`
	Legs        []Leg           `json:"legs"`
}

// New derives the profit fields from the route so a strategy cannot report
// numbers that disagree with its own legs. amountOut is net of fees.
func New(strategy, configID string, observedAt time.Time, amountIn, amountOut, fees decimal.Decimal, legs []Leg) (Opportunity, error) {
	if err := validate(strategy, configID, amountIn, legs); err != nil {
		return Opportunity{}, err
	}

	netProfit := amountOut.Sub(amountIn)
	o := Opportunity{
		Strategy:    strategy,
		ConfigID:    configID,
		ObservedAt:  observedAt,
		StartAsset:  legs[0].AssetIn,
		AmountIn:    amountIn,
		AmountOut:   amountOut,
		GrossProfit: netProfit.Add(fees),
		Fees:        fees,
		NetProfit:   netProfit,
		NetEdgeBps:  netProfit.Div(amountIn).Mul(bps),
		Legs:        legs,
	}
	o.RouteID = routeID(strategy, configID, o.StartAsset, legs)
	o.ID = sightingID(o.RouteID, observedAt)
	return o, nil
}

// routeID hashes the route rather than the observation, so every sighting of
// the same dislocation shares it and the series reads back as one history.
func routeID(strategy, configID, startAsset string, legs []Leg) string {
	parts := make([]string, 0, len(legs)+3)
	parts = append(parts, strategy, configID, startAsset)
	for _, leg := range legs {
		parts = append(parts, fmt.Sprintf("%s|%s|%s", leg.Venue, leg.Symbol, leg.Side))
	}
	return digest(parts...)
}

// sightingID is derived rather than assigned by the database so re-storing a
// tick is a no-op instead of a duplicate row.
func sightingID(routeID string, observedAt time.Time) string {
	return digest(routeID, strconv.FormatInt(observedAt.UTC().UnixNano(), 10))
}

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func validate(strategy, configID string, amountIn decimal.Decimal, legs []Leg) error {
	var errs []error
	if strategy == "" {
		errs = append(errs, errors.New("strategy is required"))
	}
	if configID == "" {
		errs = append(errs, errors.New("config id is required"))
	}
	if !amountIn.IsPositive() {
		errs = append(errs, fmt.Errorf("amount in must be positive, got %s", amountIn))
	}
	if len(legs) == 0 {
		return errors.Join(append(errs, errors.New("route has no legs"))...)
	}

	for i, leg := range legs {
		if leg.Venue == "" || leg.Symbol == "" {
			errs = append(errs, fmt.Errorf("leg %d: venue and symbol are required", i))
		}
		if leg.Side != Buy && leg.Side != Sell {
			errs = append(errs, fmt.Errorf("leg %d: unknown side %q", i, leg.Side))
		}
		if leg.AssetIn == "" || leg.AssetOut == "" {
			errs = append(errs, fmt.Errorf("leg %d: asset in and out are required", i))
		}
		if i > 0 && leg.AssetIn != legs[i-1].AssetOut {
			errs = append(errs, fmt.Errorf("leg %d takes %s but leg %d yields %s", i, leg.AssetIn, i-1, legs[i-1].AssetOut))
		}
	}

	if last := legs[len(legs)-1]; last.AssetOut != legs[0].AssetIn {
		errs = append(errs, fmt.Errorf("route starts in %s but ends in %s", legs[0].AssetIn, last.AssetOut))
	}
	return errors.Join(errs...)
}
