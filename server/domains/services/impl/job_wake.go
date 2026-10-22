package impl

import (
	"context"
	"time"
)

// wakeWhenDue asks the poll loop to look for work as soon as the transaction
// that queued a job commits, when that job is already due.
//
// A service task is queued to run now, and then waited for the next tick: two
// seconds at the default interval, on every step. Three service tasks in a row
// took six seconds to reach the person waiting after them, all of it spent
// idle. Waking the loop on commit, never before it, means the worker looks for
// a row that is there to be found: a rolled-back step wakes nobody, and a step
// on another replica is still picked up by that replica's tick.
//
// A job due later — a timer, a retry backing off — waits for the tick as
// before. The channel holds one wake, so a burst of commits costs one extra
// round rather than one per job, and the bound on rounds per tick
// (processPendingJobs) still applies to each.
func (s *jobService) wakeWhenDue(ctx context.Context, runAt time.Time) {
	if runAt.After(time.Now()) {
		return
	}
	s.repo.UnitOfWork().AfterCommit(ctx, s.wakeWorker)
}

// wakeWorker never blocks: a wake already pending covers this one, and a
// process whose workers are not running simply has nobody to wake.
func (s *jobService) wakeWorker() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
