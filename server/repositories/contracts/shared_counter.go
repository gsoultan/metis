package contracts

import (
	"context"
	"time"
)

// SharedCounterRepository holds counts that every replica contributes to and
// every replica reads.
type SharedCounterRepository interface {
	// Record writes this replica's own total for a window. Absolute, not an
	// increment: the replica owns its row, so there is nothing to lose to a
	// concurrent update.
	Record(ctx context.Context, scope, key, replica string, windowStart time.Time, count int64) error

	// Totals sums every replica's contribution for the given keys in a window.
	// One query for many keys, because a limiter asking per key would issue a
	// query per client.
	Totals(ctx context.Context, scope string, keys []string, windowStart time.Time) (map[string]int64, error)

	// Prune removes windows that have closed.
	Prune(ctx context.Context, before time.Time) (int64, error)
}
