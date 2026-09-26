package impl

import (
	"fmt"
	"sync"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The clock a simulation runs on.
//
// It belongs to the simulation rather than to the engine, and that is a
// deliberate limit on the feature's blast radius. Threading a Clock interface
// through the engine would touch every file that calls time.Now() — twenty-eight
// of them under server/domains — for a benefit the simulation mostly does not
// need: those calls stamp CreatedAt and Timestamp on rows the run rolls back.
// What actually has to be virtual is *waiting*, and waiting happens in the job
// service, which a simulation replaces outright.
//
// The residual case, stated plainly because it is real: a gateway condition or
// script that reads the current time sees the wall clock, not this one. A
// process that branches on "is it past month end?" will simulate as though it
// is being run today. Closing that gap is what the Clock seam in the engine
// would be for; nothing else here needs it.
type virtualClock struct {
	mu  sync.Mutex
	now time.Time
}

func newVirtualClock(start time.Time) *virtualClock {
	return &virtualClock{now: start}
}

func (c *virtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AdvanceISO moves the clock forward by a BPMN timer expression and reports how
// far it moved.
//
// ISO-8601 via entities.ParseTimerExpression, which is the same parser the real
// timer path uses — so "PT24H", "P1M" and a timeDate instant all mean here
// exactly what they mean to a running instance. Reimplementing duration parsing
// for the simulator is how a simulator starts disagreeing with the engine.
//
// Time never runs backwards. A timeDate already in the past means the timer is
// due now, not that the case travels to last Tuesday.
func (c *virtualClock) AdvanceISO(expr string) (time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fireAt, err := entities.ParseTimerExpression(expr, c.now)
	if err != nil {
		return 0, fmt.Errorf("simulating a timer: %w", err)
	}

	if !fireAt.After(c.now) {
		return 0, nil
	}

	elapsed := fireAt.Sub(c.now)
	c.now = fireAt
	return elapsed, nil
}

// PeekISO reports how far AdvanceISO would move without moving it.
//
// Used to decide which of two waits finishes first — a boundary timer against
// the answer for the activity it is attached to — which is a question a
// simulation can answer exactly, and a running engine can only discover.
func (c *virtualClock) PeekISO(expr string) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fireAt, err := entities.ParseTimerExpression(expr, c.now)
	if err != nil {
		return 0, false
	}
	if !fireAt.After(c.now) {
		return 0, true
	}
	return fireAt.Sub(c.now), true
}
