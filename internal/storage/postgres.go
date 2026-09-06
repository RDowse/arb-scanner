package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

// AwaitSchema blocks until the opportunities table exists, so a process can
// start alongside the migration rather than being ordered after it. Container
// runtimes disagree on how to express "wait for this one-shot to finish", so
// the wait lives here instead.
func (p *Postgres) AwaitSchema(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		var ready bool
		err := p.pool.QueryRow(ctx, "SELECT to_regclass('opportunities') IS NOT NULL").Scan(&ready)
		if err == nil && ready {
			return nil
		}

		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("wait for schema: %w", err)
			}
			return fmt.Errorf("wait for schema: opportunities table absent after %s", timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
