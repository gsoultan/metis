package replicas

import (
	"context"
	"strings"
	"testing"
	"time"

	observers "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/repositories"
	"gorm.io/gorm"
)

// A browser connected to one replica must see events produced on another.
//
// The SSE client registry holds open HTTP response writers, so it is
// necessarily per-process. That made live updates correct only for a single
// replica: scale to two behind a load balancer and roughly half of every user's
// updates went to the process they were not connected to. Nothing errored —
// the list simply stopped changing, which is the worst way for a feature to
// fail, because it looks like nothing happened rather than like a bug.
func TestAnEventOnOneReplicaReachesABrowserOnAnother(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		repo := repositories.NewRepository(db)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		producer := newFanoutReplica(t, ctx, repo, "replica-a")
		consumer := newFanoutReplica(t, ctx, repo, "replica-b")

		browser := consumer.observer.AddClient()
		defer consumer.observer.RemoveClient(browser)

		producer.observer.Broadcast(map[string]string{"type": "TaskCreated", "id": "task-1"})

		select {
		case msg := <-browser:
			if !strings.Contains(msg, "TaskCreated") || !strings.Contains(msg, "task-1") {
				t.Fatalf("the browser received %q, which is not the event that was produced", msg)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("an event produced on replica A never reached a browser on replica B: the UI would have stopped updating with nothing in any log")
		}
	})
}

// A replica delivers its own events directly, so it must not also pick them up
// off the bus — a browser would see every local event twice, and the UI turns
// each one into a refetch.
func TestAReplicaDoesNotRedeliverItsOwnEvents(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		repo := repositories.NewRepository(db)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		only := newFanoutReplica(t, ctx, repo, "replica-solo")
		browser := only.observer.AddClient()
		defer only.observer.RemoveClient(browser)

		only.observer.Broadcast(map[string]string{"type": "TaskCreated", "id": "task-1"})

		// The first is the direct local delivery.
		select {
		case <-browser:
		case <-time.After(5 * time.Second):
			t.Fatal("the replica did not deliver its own event to its own client")
		}

		// Long enough for several poll ticks to have run.
		select {
		case msg := <-browser:
			t.Fatalf("the event came back off the bus and was delivered a second time: %q", msg)
		case <-time.After(3 * time.Second):
		}
	})
}

// A replica starting up must not replay the backlog. Otherwise every restart
// floods every connected browser with invalidations for work that finished
// before it arrived — a thundering herd of refetches triggered by a deploy.
func TestAReplicaStartingUpDoesNotReplayHistory(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		repo := repositories.NewRepository(db)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Something happened before this replica existed.
		if err := repo.Broadcast().Publish(ctx, "replica-long-gone", `{"type":"TaskCreated","id":"ancient"}`); err != nil {
			t.Fatalf("seed the bus: %v", err)
		}

		latecomer := newFanoutReplica(t, ctx, repo, "replica-new")
		browser := latecomer.observer.AddClient()
		defer latecomer.observer.RemoveClient(browser)

		select {
		case msg := <-browser:
			t.Fatalf("a replica that had just started replayed history to a browser: %q", msg)
		case <-time.After(3 * time.Second):
		}
	})
}

// Pruning keeps the bus a bus. It is not an audit log — that exists separately
// — so a row that every live replica has moved past has no readers left.
func TestThePruneSweepsDeliveredEvents(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		repo := repositories.NewRepository(db)
		ctx := t.Context()

		if err := repo.Broadcast().Publish(ctx, "replica-a", `{"type":"TaskCreated"}`); err != nil {
			t.Fatalf("publish: %v", err)
		}

		// Nothing is old yet, so nothing should go.
		removed, err := repo.Broadcast().Prune(ctx, time.Now().UTC().Add(-time.Hour))
		if err != nil {
			t.Fatalf("prune: %v", err)
		}
		if removed != 0 {
			t.Fatalf("pruned %d rows that were still inside the retention window", removed)
		}

		// Everything is old now.
		removed, err = repo.Broadcast().Prune(ctx, time.Now().UTC().Add(time.Hour))
		if err != nil {
			t.Fatalf("prune: %v", err)
		}
		if removed == 0 {
			t.Fatal("the prune removed nothing, so the bus grows without bound")
		}
	})
}

// fanoutReplica is one server process: its own observer and fan-out, over the
// shared database.
type fanoutReplica struct {
	observer *observers.SSEObserver
}

func newFanoutReplica(t *testing.T, ctx context.Context, repo repositories.Repository, origin string) *fanoutReplica {
	t.Helper()
	observer := observers.NewSSEObserver()
	fanout := observers.NewSSEFanout(repo.Broadcast(), observer, origin)
	if err := fanout.Start(ctx); err != nil {
		t.Fatalf("start the fan-out for %s: %v", origin, err)
	}
	return &fanoutReplica{observer: observer}
}
