package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RDowse/arb-scanner/internal/opportunity"
)

// Postgres persists opportunities keyed by route, so a dislocation seen on
// many ticks stays one row.
type Postgres struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

// upsertOpportunity advances last_seen_at on every sighting but replaces the
// observation only when it beats the best edge seen, so the stored legs are the
// best sequence rather than the most recent one.
const upsertOpportunity = `
INSERT INTO opportunities (
	id, strategy, config_id, first_seen_at, last_seen_at, start_asset,
	amount_in, amount_out, gross_profit, fees, net_profit, net_edge_bps,
	max_edge_bps, legs
) VALUES ($1, $2, $3, $4, $4, $5, $6, $7, $8, $9, $10, $11, $11, $12)
ON CONFLICT (id) DO UPDATE SET
	last_seen_at = GREATEST(opportunities.last_seen_at, EXCLUDED.last_seen_at),
	max_edge_bps = GREATEST(opportunities.max_edge_bps, EXCLUDED.net_edge_bps),
	amount_in    = CASE WHEN EXCLUDED.net_edge_bps > opportunities.max_edge_bps THEN EXCLUDED.amount_in    ELSE opportunities.amount_in    END,
	amount_out   = CASE WHEN EXCLUDED.net_edge_bps > opportunities.max_edge_bps THEN EXCLUDED.amount_out   ELSE opportunities.amount_out   END,
	gross_profit = CASE WHEN EXCLUDED.net_edge_bps > opportunities.max_edge_bps THEN EXCLUDED.gross_profit ELSE opportunities.gross_profit END,
	fees         = CASE WHEN EXCLUDED.net_edge_bps > opportunities.max_edge_bps THEN EXCLUDED.fees         ELSE opportunities.fees         END,
	net_profit   = CASE WHEN EXCLUDED.net_edge_bps > opportunities.max_edge_bps THEN EXCLUDED.net_profit   ELSE opportunities.net_profit   END,
	net_edge_bps = CASE WHEN EXCLUDED.net_edge_bps > opportunities.max_edge_bps THEN EXCLUDED.net_edge_bps ELSE opportunities.net_edge_bps END,
	legs         = CASE WHEN EXCLUDED.net_edge_bps > opportunities.max_edge_bps THEN EXCLUDED.legs         ELSE opportunities.legs         END`

// Store writes a tick's opportunities in one transaction: either the whole tick
// lands or none of it does, leaving the caller free to retry.
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
		batch.Queue(upsertOpportunity,
			o.ID, o.Strategy, o.ConfigID, o.ObservedAt, o.StartAsset,
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
			return fmt.Errorf("upsert %s: %w", opps[i].ID, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close batch: %w", err)
	}

	return tx.Commit(ctx)
}
