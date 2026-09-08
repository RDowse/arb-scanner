package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

type Record struct {
	ID          string            `json:"id"`
	RouteID     string            `json:"route_id"`
	Strategy    string            `json:"strategy"`
	ConfigID    string            `json:"config_id"`
	ObservedAt  time.Time         `json:"observed_at"`
	StartAsset  string            `json:"start_asset"`
	AmountIn    decimal.Decimal   `json:"amount_in"`
	AmountOut   decimal.Decimal   `json:"amount_out"`
	GrossProfit decimal.Decimal   `json:"gross_profit"`
	Fees        decimal.Decimal   `json:"fees"`
	NetProfit   decimal.Decimal   `json:"net_profit"`
	NetEdgeBps  decimal.Decimal   `json:"net_edge_bps"`
	Legs        []opportunity.Leg `json:"legs"`
}

type Filter struct {
	Strategy string
	From     time.Time
	To       time.Time
	Limit    int
}

func (f Filter) EffectiveLimit() int {
	switch {
	case f.Limit <= 0:
		return defaultLimit
	case f.Limit > maxLimit:
		return maxLimit
	default:
		return f.Limit
	}
}

const selectColumns = `
	id, route_id, strategy, config_id, observed_at, start_asset,
	amount_in, amount_out, gross_profit, fees, net_profit, net_edge_bps, legs`

// List returns sightings newest first. A route seen on successive ticks appears
// once per tick, so the window filters select a history rather than a snapshot.
func (p *Postgres) List(ctx context.Context, f Filter) ([]Record, error) {
	rows, err := p.pool.Query(ctx, `SELECT`+selectColumns+`
		FROM opportunities
		WHERE ($1 = '' OR strategy = $1)
		  AND ($2::timestamptz IS NULL OR observed_at >= $2)
		  AND ($3::timestamptz IS NULL OR observed_at <= $3)
		ORDER BY observed_at DESC, id
		LIMIT $4`,
		f.Strategy, nullTime(f.From), nullTime(f.To), f.EffectiveLimit())
	if err != nil {
		return nil, fmt.Errorf("list opportunities: %w", err)
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list opportunities: %w", err)
	}
	return records, nil
}

// Strategies lists the strategies that have produced an opportunity.
func (p *Postgres) Strategies(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, "SELECT DISTINCT strategy FROM opportunities ORDER BY strategy")
	if err != nil {
		return nil, fmt.Errorf("list strategies: %w", err)
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan strategy: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list strategies: %w", err)
	}
	return names, nil
}

func scanRecord(rows pgx.Rows) (Record, error) {
	var r Record
	err := rows.Scan(
		&r.ID, &r.RouteID, &r.Strategy, &r.ConfigID, &r.ObservedAt, &r.StartAsset,
		&r.AmountIn, &r.AmountOut, &r.GrossProfit, &r.Fees, &r.NetProfit, &r.NetEdgeBps,
		&r.Legs,
	)
	if err != nil {
		return Record{}, fmt.Errorf("scan opportunity: %w", err)
	}
	return r, nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
