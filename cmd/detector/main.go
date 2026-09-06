package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/RDowse/arb-scanner/internal/config"
	"github.com/RDowse/arb-scanner/internal/logging"
	"github.com/RDowse/arb-scanner/internal/venue"
)

func main() {
	log := logging.New()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log); err != nil {
		log.Error("detector failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.DetectorFromEnv()
	if err != nil {
		return err
	}

	log.Info("detector starting", "strategies", cfg.StrategyPath)

	// TODO: load strategies, run migrations, start venue feeds, start eval loop.

	// A feed keeps one venue's books current; the eval loop reads them per tick.
	feeds := []interface {
		Name() string
		Run(context.Context) error
	}{
		venue.NewCoinbase(log, 10, "BTC/USD", "ETH/USD"),
		venue.NewKraken(log, 10, "BTC/USD", "ETH/USD"),
	}

	for _, feed := range feeds {
		go func() {
			if err := feed.Run(ctx); err != nil && ctx.Err() == nil {
				log.Error("feed stopped", "venue", feed.Name(), "err", err)
			}
		}()
	}

	<-ctx.Done()

	log.Info("detector stopped")
	return nil
}
