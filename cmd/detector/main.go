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
	
	log.Info("Starting venue..")
	feed := venue.NewCoinbase(log, 10, "BTC/USD", "ETH/USD")
	go func() {
		if err := feed.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("coinbase feed stopped", "err", err)
		}
	}()

	<-ctx.Done()

	log.Info("detector stopped")
	return nil
}
