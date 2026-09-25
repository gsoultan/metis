package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/sharedcounter"
)

type sharedCounterRepository struct{ conn }

// NewSharedCounterRepository returns the counters replicas pool through.
//
// Rate limits and circuit breakers are per installation, not per process. A
// limit that is quietly per-replica admits N times what was configured and
// looks exactly like one that is not.
func NewSharedCounterRepository(c *db.Conn) contracts.SharedCounterRepository {
	return &sharedCounterRepository{conn{conn: c}}
}

// Record publishes this replica's count for one window.
//
// Each replica writes its own row rather than incrementing a shared one: an
// increment is a write-write conflict between every replica on every request,
// and the reader can add up four rows far more cheaply than four replicas can
// take turns.
func (r *sharedCounterRepository) Record(ctx context.Context, scope, key, replica string, windowStart time.Time, count int64) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := sharedcounter.Create()
	ins.SetScope(scope)
	ins.SetCounterKey(key)
	ins.SetReplica(replica)
	ins.SetWindowStart(windowStart.UTC())
	ins.SetCount(count)
	ins.SetUpdatedAt(time.Now().UTC())
	ins.OnConflictScopeCounterKeyReplicaWindowStart()
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not record a shared counter: %w", err)
	}
	return nil
}

// Totals adds up every replica's count for each key in one window.
//
// Summed in Go rather than by the database. The rows are one per replica per
// key for a single window — a handful — and a declared aggregate would put the
// GROUP BY in the model layer, where it would be the only thing in it that
// exists to serve one caller.
func (r *sharedCounterRepository) Totals(ctx context.Context, scope string, keys []string, windowStart time.Time) (map[string]int64, error) {
	totals := make(map[string]int64, len(keys))
	if len(keys) == 0 {
		return totals, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := sharedcounter.New().
		Where(
			sharedcounter.Scope.Eq(scope),
			sharedcounter.WindowStart.Eq(windowStart.UTC()),
			sharedcounter.CounterKey.In(keys...),
		).
		Unordered().
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read shared counters: %w", err)
	}
	for _, row := range rows {
		totals[row.CounterKey] += row.Count
	}
	return totals, nil
}

// Prune drops windows that have passed.
//
// Raw SQL because storm deletes by primary key and this is a sweep: naming
// every row first would be a read of exactly the rows about to be deleted.
func (r *sharedCounterRepository) Prune(ctx context.Context, before time.Time) (int64, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := db.DeleteInBatches(ctx, ex,
		`DELETE FROM shared_counters WHERE ctid = ANY(ARRAY(
		     SELECT ctid FROM shared_counters WHERE window_start < $1 LIMIT $2))`, before.UTC())
	if err != nil {
		return removed, fmt.Errorf("could not prune shared counters: %w", err)
	}
	return removed, nil
}
