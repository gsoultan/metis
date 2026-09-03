package impl

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/models"
)

// slowBus stands in for a database that has stopped keeping up.
type slowBus struct{ delay time.Duration }

func (s *slowBus) Publish(ctx context.Context, _, _ string) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *slowBus) Since(context.Context, string, int64, int) ([]models.BroadcastEventModel, error) {
	return nil, nil
}
func (s *slowBus) LatestID(context.Context) (int64, error)         { return 0, nil }
func (s *slowBus) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

// Publishing must cost a bounded amount, whatever the bus is doing.
//
// It used to start a goroutine per event, which is fine while the database
// keeps up and unbounded when it does not: measured before this test existed,
// five thousand events against a slow bus produced five thousand goroutines,
// each holding a payload and a pending write. The conditions that make a
// database slow are the conditions that make an engine busy, so the failure
// arrived exactly when there was least room for it.
func TestPublishingDoesNotGrowWithoutBound(t *testing.T) {
	observer := NewSSEObserver()
	fanout := NewSSEFanout(&slowBus{delay: 3 * time.Second}, observer, "replica-under-load")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := fanout.Start(ctx); err != nil {
		t.Fatalf("start the fan-out: %v", err)
	}

	before := runtime.NumGoroutine()
	// About what one instance of a twenty-node process produces, times a few
	// hundred instances: a second or two of a busy engine.
	for range 5000 {
		observer.Broadcast(map[string]string{"type": "NodeCompleted"})
	}
	time.Sleep(300 * time.Millisecond)
	growth := runtime.NumGoroutine() - before

	// The workers are started before this is measured, so the only growth left
	// would be per-event goroutines. Generous, because the runtime has its own.
	if growth > 20 {
		t.Fatalf("publishing 5000 events added %d goroutines; it is meant to be bounded by the worker pool", growth)
	}
}

// What is given up for that bound is promptness on other replicas, and it has
// to be visible rather than silent — an operator whose users report stale lists
// needs the log to say so.
func TestOverflowIsCountedRatherThanSwallowed(t *testing.T) {
	observer := NewSSEObserver()
	fanout := NewSSEFanout(&slowBus{delay: 10 * time.Second}, observer, "replica-overflowing")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := fanout.Start(ctx); err != nil {
		t.Fatalf("start the fan-out: %v", err)
	}

	// Comfortably past the queue depth and the workers holding one each.
	for range publishQueueDepth * 3 {
		observer.Broadcast(map[string]string{"type": "NodeCompleted"})
	}

	if dropped := fanout.dropped.Load(); dropped == 0 {
		t.Fatal("nothing was recorded as dropped, so an operator seeing stale lists would have nothing to look at")
	}
}

// Local delivery is what the browsers on this replica depend on, and it must
// not be affected by a bus that has stopped answering.
func TestASlowBusDoesNotStallLocalDelivery(t *testing.T) {
	observer := NewSSEObserver()
	fanout := NewSSEFanout(&slowBus{delay: 10 * time.Second}, observer, "replica-local")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := fanout.Start(ctx); err != nil {
		t.Fatalf("start the fan-out: %v", err)
	}

	browser := observer.AddClient()
	defer observer.RemoveClient(browser)

	start := time.Now()
	observer.Broadcast(map[string]string{"type": "TaskCreated"})
	elapsed := time.Since(start)

	select {
	case <-browser:
	case <-time.After(2 * time.Second):
		t.Fatal("a browser on this replica did not receive an event while the bus was slow")
	}
	if elapsed > time.Second {
		t.Errorf("Broadcast took %v: the path that produced the event is waiting on the bus", elapsed)
	}
}
