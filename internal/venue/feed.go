package venue

import (
	"context"
	"log/slog"
	"time"
)

const (
	feedBackoffStart = time.Second
	feedBackoffMax   = 30 * time.Second
)

// runFeed keeps session running until ctx is cancelled, backing off between
// attempts. Every disconnect invalidates the cached books: without per-message
// sequencing there is no way to tell a resumed stream from a gapped one, so a
// stale book is preferred over a silently corrupt one.
func runFeed(ctx context.Context, log *slog.Logger, session func(context.Context) error, invalidate func()) error {
	backoff := feedBackoffStart

	for {
		err := session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		invalidate()
		log.Warn("feed dropped, reconnecting", "err", err, "retry_in", backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		if backoff *= 2; backoff > feedBackoffMax {
			backoff = feedBackoffMax
		}
	}
}
