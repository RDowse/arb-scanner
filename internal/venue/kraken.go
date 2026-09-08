package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/shopspring/decimal"

	"github.com/RDowse/arb-scanner/internal/market"
)

const (
	KrakenName = "kraken"
	krakenURL  = "wss://ws.kraken.com/v2"

	krakenReadLimit   = 8 << 20
	krakenReadTimeout = 15 * time.Second

	// Kraken checksums the top ten levels of each side.
	krakenChecksumDepth = 10
)

// krakenDepths are the only book depths the feed accepts.
var krakenDepths = []int{10, 25, 100, 500, 1000}

type Kraken struct {
	url     string
	symbols []string

	// depth is what callers asked for; feedDepth is what Kraken will serve.
	depth     int
	feedDepth int

	log *slog.Logger

	mu    sync.RWMutex
	books map[string]*bookState

	// precision comes from the instrument channel and is what makes a checksum
	// reproducible: the book channel sends 50000.10 as 50000.1, so the trailing
	// digits have to be restored before hashing.
	precision map[string]krakenPrecision
}

type krakenPrecision struct {
	price int32
	qty   int32
}

func NewKraken(log *slog.Logger, depth int, symbols ...string) *Kraken {
	books := make(map[string]*bookState, len(symbols))
	for _, s := range symbols {
		books[s] = newBookState()
	}
	return &Kraken{
		url:       krakenURL,
		symbols:   symbols,
		depth:     depth,
		feedDepth: krakenFeedDepth(depth),
		log:       log.With("venue", KrakenName),
		books:     books,
		precision: make(map[string]krakenPrecision, len(symbols)),
	}
}

func (k *Kraken) Name() string { return KrakenName }

func (k *Kraken) Books() []market.Book {
	k.mu.RLock()
	defer k.mu.RUnlock()

	out := make([]market.Book, 0, len(k.books))
	for symbol, state := range k.books {
		out = append(out, state.book(KrakenName, symbol, k.depth))
	}
	return out
}

func (k *Kraken) Run(ctx context.Context) error {
	return runFeed(ctx, k.log, k.session, k.invalidate)
}

func (k *Kraken) session(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, k.url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(krakenReadLimit)

	if err := k.subscribe(ctx, conn); err != nil {
		return err
	}
	k.log.Info("subscribed", "symbols", k.symbols, "depth", k.feedDepth)

	for {
		readCtx, cancel := context.WithTimeout(ctx, krakenReadTimeout)
		_, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if err := k.handle(data); err != nil {
			return fmt.Errorf("handle: %w", err)
		}
	}
}

func (k *Kraken) subscribe(ctx context.Context, conn *websocket.Conn) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// The instrument snapshot carries the decimal precision the checksum needs,
	// so it has to be subscribed before the book.
	requests := []any{
		map[string]any{
			"method": "subscribe",
			"params": map[string]any{"channel": "instrument", "snapshot": true},
		},
		map[string]any{
			"method": "subscribe",
			"params": map[string]any{
				"channel":  "book",
				"symbol":   k.symbols,
				"depth":    k.feedDepth,
				"snapshot": true,
			},
		},
	}

	for _, req := range requests {
		payload, err := json.Marshal(req)
		if err != nil {
			return err
		}
		if err := conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
			return fmt.Errorf("subscribe: %w", err)
		}
	}
	return nil
}

type krakenLevel struct {
	Price json.Number `json:"price"`
	Qty   json.Number `json:"qty"`
}

type krakenBook struct {
	Symbol    string        `json:"symbol"`
	Bids      []krakenLevel `json:"bids"`
	Asks      []krakenLevel `json:"asks"`
	Checksum  uint32        `json:"checksum"`
	Timestamp time.Time     `json:"timestamp"`
}

func (k *Kraken) handle(data []byte) error {
	var msg struct {
		Channel string          `json:"channel"`
		Type    string          `json:"type"`
		Method  string          `json:"method"`
		Success *bool           `json:"success"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	if msg.Method != "" {
		if msg.Success != nil && !*msg.Success {
			return fmt.Errorf("%s rejected: %s", msg.Method, msg.Error)
		}
		return nil
	}

	switch msg.Channel {
	case "book":
		var books []krakenBook
		if err := json.Unmarshal(msg.Data, &books); err != nil {
			return fmt.Errorf("decode book: %w", err)
		}
		for _, b := range books {
			if err := k.applyBook(b, msg.Type == "snapshot"); err != nil {
				return err
			}
		}
		return nil

	case "instrument":
		return k.applyInstruments(msg.Data)

	default:
		return nil
	}
}

// applyBook replaces the book on a snapshot and edits it on an update.
func (k *Kraken) applyBook(b krakenBook, snapshot bool) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	state, ok := k.books[b.Symbol]
	if !ok {
		return nil
	}
	if snapshot {
		state.reset()
	}

	for _, level := range b.Bids {
		if err := state.set(bid, level.Price.String(), level.Qty.String()); err != nil {
			return fmt.Errorf("book %s: %w", b.Symbol, err)
		}
	}
	for _, level := range b.Asks {
		if err := state.set(ask, level.Price.String(), level.Qty.String()); err != nil {
			return fmt.Errorf("book %s: %w", b.Symbol, err)
		}
	}
	if err := k.verifyChecksum(state, b); err != nil {
		return err
	}

	state.updatedAt = b.Timestamp
	if state.updatedAt.IsZero() {
		state.updatedAt = time.Now().UTC()
	}
	return nil
}

// verifyChecksum is the only gap detection Kraken offers: a mismatch means the
// book has diverged, which the caller turns into a reconnect and a fresh
// snapshot. Skipped until the instrument snapshot has supplied the precision.
func (k *Kraken) verifyChecksum(state *bookState, b krakenBook) error {
	if b.Checksum == 0 {
		return nil
	}
	p, ok := k.precision[b.Symbol]
	if !ok {
		return nil
	}

	if got := krakenChecksum(state, p); got != b.Checksum {
		return fmt.Errorf("book %s: checksum %d, want %d", b.Symbol, got, b.Checksum)
	}
	return nil
}

func krakenChecksum(state *bookState, p krakenPrecision) uint32 {
	var sb strings.Builder

	for _, level := range sortedLevels(state.asks, false, krakenChecksumDepth) {
		sb.WriteString(krakenChecksumToken(level.Price, p.price))
		sb.WriteString(krakenChecksumToken(level.Size, p.qty))
	}
	for _, level := range sortedLevels(state.bids, true, krakenChecksumDepth) {
		sb.WriteString(krakenChecksumToken(level.Price, p.price))
		sb.WriteString(krakenChecksumToken(level.Size, p.qty))
	}

	return crc32.ChecksumIEEE([]byte(sb.String()))
}

// krakenChecksumToken renders a value at the pair's precision, then drops the
// decimal point and leading zeros, as Kraken specifies.
func krakenChecksumToken(d decimal.Decimal, places int32) string {
	s := strings.Replace(d.StringFixed(places), ".", "", 1)
	if s = strings.TrimLeft(s, "0"); s == "" {
		return "0"
	}
	return s
}

func (k *Kraken) applyInstruments(data json.RawMessage) error {
	var instruments struct {
		Pairs []struct {
			Symbol         string `json:"symbol"`
			PricePrecision int32  `json:"price_precision"`
			QtyPrecision   int32  `json:"qty_precision"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(data, &instruments); err != nil {
		return fmt.Errorf("decode instruments: %w", err)
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	for _, pair := range instruments.Pairs {
		if _, tracked := k.books[pair.Symbol]; !tracked {
			continue
		}
		k.precision[pair.Symbol] = krakenPrecision{price: pair.PricePrecision, qty: pair.QtyPrecision}
	}
	return nil
}

func (k *Kraken) invalidate() {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, state := range k.books {
		state.reset()
	}
}

// krakenFeedDepth rounds up to a depth the feed accepts, so a book is never
// shallower than the caller asked for.
func krakenFeedDepth(depth int) int {
	for _, allowed := range krakenDepths {
		if depth <= allowed {
			return allowed
		}
	}
	return krakenDepths[len(krakenDepths)-1]
}
