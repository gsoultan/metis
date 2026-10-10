package migrations

import (
	"context"

	"gorm.io/gorm"
)

// SharedCounterWindowIndexMigration is the version the index the rate-limit
// sweep reads through is recorded under. 33 is the instance ledger; versions
// are identities, and a number taken twice stops the server booting.
const SharedCounterWindowIndexMigration = 34

// sharedCounterWindowIndex is migration 34: an index on
// shared_counters.window_start, the column the retention sweep cuts by.
//
// The sweep deletes the windows that started more than five minutes ago, five
// thousand rows a statement, every ten minutes on every replica. window_start
// is the last column of the primary key — (scope, counter_key, replica,
// window_start) — so the key's index cannot answer "every row before this
// moment", and each batch was a scan of the whole table: one per five
// thousand rows removed, the most of them after a burst of traffic, when the
// table is largest.
//
// An index on window_start rather than a sweep by updated_at, which is
// indexed already. The two are not the same question. updated_at is when a
// replica last flushed its count; the retention rule is about when the window
// started. With one-minute windows they pick nearly the same rows, but not the
// same ones — a window's last flush can land up to a window's length after it
// started, and a replica that stops counting mid-window stops flushing it —
// so switching would change which rows a sweep removes in exchange for an
// index. Indexing the column the rule is written in changes nothing but the
// plan.
//
// Built CONCURRENTLY, as 20: every request through a rate limit flushes into
// this table, and a plain build's lock would hold up every one of them for as
// long as the build took.
func sharedCounterWindowIndex() Migration {
	return Migration{
		Version: SharedCounterWindowIndexMigration,
		Name:    "find the rate-limit windows that have closed from an index",
		Run: func(ctx context.Context, db *gorm.DB) error {
			return createIndexConcurrently(ctx, db,
				"shared_counters", "ix_shared_counters_window_start", "window_start")
		},
	}
}
