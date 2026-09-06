package detector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/market"
	"github.com/RDowse/arb-scanner/internal/opportunity"
	"github.com/RDowse/arb-scanner/internal/strategy"
)

func dec(t *testing.T, s string) decimal.Decimal {
	t.Helper()

	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal %q: %v", s, err)
	}
	return d
}

// stubFeed serves fixed books and records whether it was started.
type stubFeed struct {
	name  string
	books []market.Book

	mu      sync.Mutex
	started bool
}

func (f *stubFeed) Name() string { return f.name }

func (f *stubFeed) Books() []market.Book { return f.books }

func (f *stubFeed) Run(ctx context.Context) error {
	f.mu.Lock()
	f.started = true
	f.mu.Unlock()

	<-ctx.Done()
	return ctx.Err()
}

func (f *stubFeed) wasStarted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started
}

type stubSink struct {
	mu     sync.Mutex
	calls  int
	stored []opportunity.Opportunity
	err    error
}

func (s *stubSink) Store(_ context.Context, opps []opportunity.Opportunity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++
	s.stored = append(s.stored, opps...)
	return s.err
}

func (s *stubSink) snapshot() (int, []opportunity.Opportunity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]opportunity.Opportunity(nil), s.stored...)
}

// failingStrategy stands in for a strategy that errors on a tick.
type failingStrategy struct{ err error }

func (f failingStrategy) Name() string { return "failing" }

func (f failingStrategy) Generate([]market.Book) ([]opportunity.Opportunity, error) {
	return nil, f.err
}

func crossingBooks(t *testing.T, at time.Time) ([]market.Book, []market.Book) {
	t.Helper()

	kraken := market.Book{
		Venue: "kraken", Symbol: "BTC/USD", UpdatedAt: at,
		Asks: []market.Level{{Price: dec(t, "50000"), Size: dec(t, "1")}},
	}
	coinbase := market.Book{
		Venue: "coinbase", Symbol: "BTC/USD", UpdatedAt: at,
		Bids: []market.Level{{Price: dec(t, "50500"), Size: dec(t, "1")}},
	}
	return []market.Book{kraken}, []market.Book{coinbase}
}

func testDetector(t *testing.T, sink Sink, strategies []strategy.Strategy) (*Detector, *stubFeed, *stubFeed) {
	t.Helper()

	now := time.Now()
	krakenBooks, coinbaseBooks := crossingBooks(t, now)
	kraken := &stubFeed{name: "kraken", books: krakenBooks}
	coinbase := &stubFeed{name: "coinbase", books: coinbaseBooks}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(log, time.Millisecond, []Feed{kraken, coinbase}, strategies, sink)
	return d, kraken, coinbase
}

func crossVenue() *strategy.CrossVenue {
	return &strategy.CrossVenue{
		ConfigID: "v1",
		TakerFees: map[string]decimal.Decimal{
			"kraken":   decimal.Zero,
			"coinbase": decimal.Zero,
		},
		MaxBookAge: time.Minute,
	}
}

func TestEvaluate(t *testing.T) {
	t.Run("stores what the strategies find across every feed", func(t *testing.T) {
		sink := &stubSink{}
		d, _, _ := testDetector(t, sink, []strategy.Strategy{crossVenue()})

		d.evaluate(context.Background())

		calls, stored := sink.snapshot()
		if calls != 1 {
			t.Fatalf("sink called %d times, want 1", calls)
		}
		if len(stored) != 1 {
			t.Fatalf("stored %d opportunities, want 1: books from separate feeds must be evaluated together", len(stored))
		}

		got := stored[0]
		if got.Legs[0].Venue != "kraken" || got.Legs[1].Venue != "coinbase" {
			t.Errorf("route = buy %s sell %s, want buy kraken sell coinbase", got.Legs[0].Venue, got.Legs[1].Venue)
		}
		if !got.NetProfit.Equal(dec(t, "500")) {
			t.Errorf("net profit = %s, want 500", got.NetProfit)
		}
	})

	t.Run("does not write when nothing is found", func(t *testing.T) {
		sink := &stubSink{}
		s := crossVenue()
		s.MinEdgeBps = decimal.NewFromInt(1000)
		d, _, _ := testDetector(t, sink, []strategy.Strategy{s})

		d.evaluate(context.Background())

		if calls, _ := sink.snapshot(); calls != 0 {
			t.Errorf("sink called %d times, want none when there is nothing to store", calls)
		}
	})

	t.Run("keeps going when one strategy fails", func(t *testing.T) {
		sink := &stubSink{}
		strategies := []strategy.Strategy{
			failingStrategy{err: errors.New("boom")},
			crossVenue(),
		}
		d, _, _ := testDetector(t, sink, strategies)

		d.evaluate(context.Background())

		if _, stored := sink.snapshot(); len(stored) != 1 {
			t.Errorf("stored %d opportunities, want the working strategy's 1", len(stored))
		}
	})

	t.Run("survives a write failure", func(t *testing.T) {
		sink := &stubSink{err: errors.New("connection refused")}
		d, _, _ := testDetector(t, sink, []strategy.Strategy{crossVenue()})

		d.evaluate(context.Background())
		d.evaluate(context.Background())

		if calls, _ := sink.snapshot(); calls != 2 {
			t.Errorf("sink called %d times, want 2: a failed write must not stop the next tick", calls)
		}
	})

	t.Run("skips evaluation when no feed has books", func(t *testing.T) {
		sink := &stubSink{}
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		d := New(log, time.Millisecond, []Feed{&stubFeed{name: "kraken"}}, []strategy.Strategy{crossVenue()}, sink)

		d.evaluate(context.Background())

		if calls, _ := sink.snapshot(); calls != 0 {
			t.Errorf("sink called %d times, want none", calls)
		}
	})

	t.Run("ranks across strategies before storing", func(t *testing.T) {
		sink := &stubSink{}
		wide, narrow := crossVenue(), crossVenue()
		narrow.ConfigID = "v2"
		d, _, _ := testDetector(t, sink, []strategy.Strategy{narrow, wide})

		d.evaluate(context.Background())

		_, stored := sink.snapshot()
		if len(stored) != 2 {
			t.Fatalf("stored %d opportunities, want 2", len(stored))
		}
		if stored[0].NetEdgeBps.LessThan(stored[1].NetEdgeBps) {
			t.Errorf("edges out of order: %s then %s", stored[0].NetEdgeBps, stored[1].NetEdgeBps)
		}
	})
}

func TestRun(t *testing.T) {
	sink := &stubSink{}
	d, kraken, coinbase := testDetector(t, sink, []strategy.Strategy{crossVenue()})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := d.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v, want the context error", err)
	}

	if !kraken.wasStarted() || !coinbase.wasStarted() {
		t.Error("Run did not start every feed")
	}
	if calls, _ := sink.snapshot(); calls == 0 {
		t.Error("no ticks evaluated in 100ms at a 1ms interval")
	}
}
