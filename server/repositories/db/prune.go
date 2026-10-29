package db

import (
	"context"
	"fmt"
)

// PruneBatch is the most rows one statement of a retention sweep deletes.
//
// The first sweep after an upgrade meets everything a table has collected
// since it was created. One DELETE for all of it is one transaction holding
// every one of those row locks and writing the lot to the WAL before anything
// commits — on tables the request path inserts into. A batch commits and lets
// go before the next one starts.
const PruneBatch = 5000

// PruneInBatches runs batch until a run removes fewer than PruneBatch rows,
// and returns how many went in all.
//
// batch removes at most limit rows, which is passed PruneBatch: a DeleteBatch
// (or HardDeleteBatch) with that Limit, which picks the rows by ctid. Not by
// key: `id IN (SELECT id ... LIMIT $n)` plans as a hash join against the whole
// table, so every batch reads all of it and clearing a backlog costs the
// square of its size; by ctid, a batch reads the rows it removes. Run outside
// a transaction, each batch commits on its own. That is the point, so a sweep
// must not be started inside a unit of work.
func PruneInBatches(ctx context.Context, batch func(limit int64) (int64, error)) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		removed, err := batch(PruneBatch)
		if err != nil {
			return total, fmt.Errorf("delete in batches: %w", err)
		}
		total += removed
		if removed < PruneBatch {
			return total, nil
		}
	}
}
