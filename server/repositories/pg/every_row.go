package pg

import (
	"context"

	"github.com/gsoultan/storm/runtime"
)

// everyRowBatch is how many rows a read that has to see everything takes at a
// time. The same as the store's own default, so a read that used to stop at a
// thousand now takes the thousand it always took and then asks for the next.
const everyRowBatch = 1000

// keysetQuery is the part of a generated query a keyset walk needs.
type keysetQuery[R any, Q any] interface {
	Limit(n int64) Q
	After(r R) Q
	All(ctx context.Context, ex runtime.Executor, dst []R) ([]R, error)
}

// everyRow returns every row q matches, a batch at a time.
//
// A generated query starts with a limit of a thousand rows (storm's New). That
// is a guard on one read, not a statement about the table: a read that relies on
// it gets the first thousand rows and no sign that there were more. Right for a
// screen, and wrong for anything that acts on every row it is given — a signal
// owed to every waiting instance, a migration of every running one — which is
// what this is for.
//
// It pages on the query's order with a keyset cursor, as ScanWithGraphs does,
// so the order has to be unique for the cursor to be a position rather than a
// tie. The default, the primary key, is; an explicit order must end in one.
func everyRow[R any, Q keysetQuery[R, Q]](ctx context.Context, ex runtime.Executor, q Q) ([]R, error) {
	var rows []R
	next := q.Limit(everyRowBatch)
	for {
		batch, err := next.All(ctx, ex, nil)
		if err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
		if len(batch) < everyRowBatch {
			return rows, nil
		}
		next = q.Limit(everyRowBatch).After(batch[len(batch)-1])
	}
}
