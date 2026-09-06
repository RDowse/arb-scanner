// Package detector joins the venue feeds to the strategies: every tick it takes
// the books each feed currently holds, asks each strategy what it finds in
// them, and persists the result.
package detector

import (
	"context"
	"log/slog"
	"time"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
	"github.com/RDowse/arb-scanner/internal/strategy"
)

// storeTimeout bounds one tick's write so a slow database cannot stall
// evaluation indefinitely.
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

// Run starts every feed and evaluates on each tick until ctx is cancelled. A
// feed that dies takes only its own books with it: the remaining venues keep
// being evaluated, and routes touching the dead one simply stop appearing.
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

// evaluate runs one tick. Nothing here is fatal: a strategy that errors and a
// write that fails are both logged and left for the next tick, because the
// alternative is a detector that stops watching the market.
func (d *Detector) evaluate(ctx context.Context) {
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
		found = append(found, opps...)
	}

	if len(found) == 0 {
		d.log.Debug("no opportunities", "books", len(books))
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
