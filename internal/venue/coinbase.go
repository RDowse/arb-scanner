package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/RDowse/arb-scanner/internal/market"
)

const (
	CoinbaseName = "coinbase"
	coinbaseURL  = "wss://ws-feed.exchange.coinbase.com"

	// The level2 snapshot is the whole book, far past the 32KiB default.
	coinbaseReadLimit = 32 << 20

	// heartbeat arrives every second, so silence past this means a dead link
	// that never produced a TCP error.
	coinbaseReadTimeout = 15 * time.Second
)

type Coinbase struct {
	url     string
	symbols []string
	depth   int
	log     *slog.Logger

	mu    sync.RWMutex
	books map[string]*bookState
}

func NewCoinbase(log *slog.Logger, depth int, symbols ...string) *Coinbase {
	books := make(map[string]*bookState, len(symbols))
	for _, s := range symbols {
		books[s] = newBookState()
	}
	return &Coinbase{
		url:     coinbaseURL,
		symbols: symbols,
		depth:   depth,
		log:     log.With("venue", CoinbaseName),
		books:   books,
	}
}

func (c *Coinbase) Name() string { return CoinbaseName }

func (c *Coinbase) Books() []market.Book {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]market.Book, 0, len(c.books))
	for symbol, state := range c.books {
		out = append(out, state.book(CoinbaseName, symbol, c.depth))
	}
	return out
}

// Run maintains the subscription until ctx is cancelled, reconnecting with
// backoff. Every disconnect drops the cached books: without per-message
// sequencing there is no way to tell a resumed stream from a gapped one, so a
// stale book is preferred over a silently corrupt one.
func (c *Coinbase) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		c.invalidate()
		c.log.Warn("feed dropped, reconnecting", "err", err, "retry_in", backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (c *Coinbase) session(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(coinbaseReadLimit)

	if err := c.subscribe(ctx, conn); err != nil {
		return err
	}
	c.log.Info("subscribed", "symbols", c.symbols)

	for {
		readCtx, cancel := context.WithTimeout(ctx, coinbaseReadTimeout)
		_, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if err := c.handle(data); err != nil {
			return fmt.Errorf("handle: %w", err)
		}
	}
}

func (c *Coinbase) subscribe(ctx context.Context, conn *websocket.Conn) error {
	products := make([]string, len(c.symbols))
	for i, s := range c.symbols {
		products[i] = coinbaseProduct(s)
	}

	req := struct {
		Type       string   `json:"type"`
		ProductIDs []string `json:"product_ids"`
		Channels   []string `json:"channels"`
	}{
		Type:       "subscribe",
		ProductIDs: products,
		Channels:   []string{"level2_batch", "heartbeat"},
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}

	// The server disconnects if no subscription arrives within 5s.
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, payload)
}

func (c *Coinbase) handle(data []byte) error {
	var msg struct {
		Type      string     `json:"type"`
		ProductID string     `json:"product_id"`
		Message   string     `json:"message"`
		Reason    string     `json:"reason"`
		Time      time.Time  `json:"time"`
		Bids      [][]string `json:"bids"`
		Asks      [][]string `json:"asks"`
		Changes   [][]string `json:"changes"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	switch msg.Type {
	case "snapshot":
		return c.applySnapshot(msg.ProductID, msg.Bids, msg.Asks)
	case "l2update":
		return c.applyChanges(msg.ProductID, msg.Changes, msg.Time)
	case "error":
		return fmt.Errorf("feed error: %s: %s", msg.Message, msg.Reason)
	default:
		return nil
	}
}

func (c *Coinbase) applySnapshot(product string, bids, asks [][]string) error {
	symbol := canonicalSymbol(product)

	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.books[symbol]
	if !ok {
		return nil
	}
	state.reset()

	for _, level := range bids {
		if len(level) < 2 {
			return fmt.Errorf("snapshot %s: malformed bid %v", product, level)
		}
		if err := state.set(bid, level[0], level[1]); err != nil {
			return fmt.Errorf("snapshot %s: %w", product, err)
		}
	}
	for _, level := range asks {
		if len(level) < 2 {
			return fmt.Errorf("snapshot %s: malformed ask %v", product, level)
		}
		if err := state.set(ask, level[0], level[1]); err != nil {
			return fmt.Errorf("snapshot %s: %w", product, err)
		}
	}
	state.updatedAt = time.Now().UTC()
	return nil
}

func (c *Coinbase) applyChanges(product string, changes [][]string, ts time.Time) error {
	symbol := canonicalSymbol(product)

	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.books[symbol]
	if !ok {
		return nil
	}

	for _, change := range changes {
		if len(change) < 3 {
			return fmt.Errorf("update %s: malformed change %v", product, change)
		}

		s := bid
		if change[0] == "sell" {
			s = ask
		}
		if err := state.set(s, change[1], change[2]); err != nil {
			return fmt.Errorf("update %s: %w", product, err)
		}
	}

	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	state.updatedAt = ts
	return nil
}

func (c *Coinbase) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, state := range c.books {
		state.reset()
	}
}

func coinbaseProduct(symbol string) string {
	return strings.ReplaceAll(symbol, "/", "-")
}

func canonicalSymbol(product string) string {
	return strings.ReplaceAll(product, "-", "/")
}
