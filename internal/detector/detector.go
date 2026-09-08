package detector

import (
	"context"
	"log/slog"
	"time"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
	"github.com/RDowse/arb-scanner/internal/strategy"
)

const storeTimeout = 10 * time.Second

type Feed interface {
	Name() string
	Books() []market.Book
	Run(context.Context) error
}

type Sink interface {
	Store(context.Context, []opportunity.Opportunity) error
}

type Detector struct {
	feeds      []Feed
	strategies []strategy.Strategy
	sink       Sink
	interval   time.Duration
	log        *slog.Logger
}

func New(log *slog.Logger, interval time.Duration, feeds []Feed, strategies []strategy.Strategy, sink Sink) *Detector {
	return &Detector{
		feeds:      feeds,
		strategies: strategies,
		sink:       sink,
		interval:   interval,
		log:        log,
	}
}

func (d *Detector) Run(ctx context.Context) error {
	for _, feed := range d.feeds {
		go func() {
			if err := feed.Run(ctx); err != nil && ctx.Err() == nil {
				d.log.Error("feed stopped", "venue", feed.Name(), "err", err)
			}
		}()
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.log.Info("evaluating", "interval", d.interval, "feeds", len(d.feeds), "strategies", len(d.strategies))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			d.evaluate(ctx)
		}
	}
}

func (d *Detector) evaluate(ctx context.Context) {
	started := time.Now()

	books := d.books()
	if len(books) == 0 {
		d.log.Debug("no books to evaluate")
		return
	}

	var found []opportunity.Opportunity
	for _, s := range d.strategies {
		opps, err := s.Generate(books)
		if err != nil {
			d.log.Error("strategy failed", "strategy", s.Name(), "err", err)
			continue
		}
		d.log.Debug("strategy evaluated", "strategy", s.Name(), "opportunities", len(opps))
		found = append(found, opps...)
	}

	d.log.Debug("tick", "books", len(books), "opportunities", len(found), "took", time.Since(started).Round(time.Microsecond))

	if len(found) == 0 {
		return
	}
	strategy.Rank(found)

	storeCtx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()

	if err := d.sink.Store(storeCtx, found); err != nil {
		d.log.Error("store opportunities", "count", len(found), "err", err)
		return
	}

	best := found[0]
	d.log.Info("opportunities found",
		"count", len(found),
		"books", len(books),
		"best_strategy", best.Strategy,
		"best_edge_bps", best.NetEdgeBps.StringFixed(2),
		"best_net_profit", best.NetProfit.StringFixed(2),
		"best_start_asset", best.StartAsset,
	)
}

func (d *Detector) books() []market.Book {
	var books []market.Book
	for _, feed := range d.feeds {
		books = append(books, feed.Books()...)
	}
	return books
}
