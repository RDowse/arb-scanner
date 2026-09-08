package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

const insertOpportunity = `
INSERT INTO opportunities (
	id, route_id, strategy, config_id, observed_at, start_asset,
	amount_in, amount_out, gross_profit, fees, net_profit, net_edge_bps, legs
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (id) DO NOTHING`

func (p *Postgres) Store(ctx context.Context, opps []opportunity.Opportunity) error {
	if len(opps) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, o := range opps {
		legs, err := json.Marshal(o.Legs)
		if err != nil {
			return fmt.Errorf("marshal legs of %s: %w", o.ID, err)
		}
		batch.Queue(insertOpportunity,
			o.ID, o.RouteID, o.Strategy, o.ConfigID, o.ObservedAt, o.StartAsset,
			o.AmountIn, o.AmountOut, o.GrossProfit, o.Fees, o.NetProfit,
			o.NetEdgeBps, legs,
		)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	results := tx.SendBatch(ctx, batch)
	for i := range opps {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("insert %s: %w", opps[i].ID, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close batch: %w", err)
	}

	return tx.Commit(ctx)
}
