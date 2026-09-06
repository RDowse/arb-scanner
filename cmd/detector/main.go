package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/config"
	"github.com/RDowse/arb-scanner/internal/detector"
	"github.com/RDowse/arb-scanner/internal/logging"
	"github.com/RDowse/arb-scanner/internal/storage"
	"github.com/RDowse/arb-scanner/internal/strategy"
	"github.com/RDowse/arb-scanner/internal/venue"
)

// Mirrors config/strategies.yaml, which is not yet loaded: taker fees at each
// venue's lowest 30-day volume tier, and the thresholds an opportunity must
// clear to be worth recording.
const (
	configID = "v1"

	bookDepth    = 10
	krakenFee    = "0.0026"
	coinbaseFee  = "0.0040"
	minEdgeBps   = "5"
	minSizeQuote = "100"
)

var symbols = []string{"BTC/USD", "ETH/USD"}

func main() {
	log := logging.New()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("detector failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.DetectorFromEnv()
	if err != nil {
		return err
	}

	db, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	feeds := []detector.Feed{
		venue.NewCoinbase(log, bookDepth, symbols...),
		venue.NewKraken(log, bookDepth, symbols...),
	}

	strategies := []strategy.Strategy{
		&strategy.CrossVenue{
			ConfigID: configID,
			TakerFees: map[string]decimal.Decimal{
				venue.KrakenName:   decimal.RequireFromString(krakenFee),
				venue.CoinbaseName: decimal.RequireFromString(coinbaseFee),
			},
			MinEdgeBps:   decimal.RequireFromString(minEdgeBps),
			MinSizeQuote: decimal.RequireFromString(minSizeQuote),
			MaxBookAge:   cfg.MaxBookAge,
		},
	}

	log.Info("detector starting",
		"symbols", symbols,
		"eval_interval", cfg.EvalInterval,
		"max_book_age", cfg.MaxBookAge,
	)

	err = detector.New(log, cfg.EvalInterval, feeds, strategies, db).Run(ctx)

	log.Info("detector stopped")
	return err
}
