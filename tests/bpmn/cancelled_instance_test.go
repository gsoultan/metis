package bpmn_test

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// What a cancel takes with the instance.
//
// A cancel ends an instance somebody decided to end, and is the one ending
// that can take what the instance had out with it: it holds the instance, and
// it knows the instance will not run again. The tasks in people's inboxes and
// the events the instance waited for went already. The work it had parked for
// outside workers did not: it stayed on the list workers fetch from until one
// of them reported on it.

// BPMN 2.0.2 §13.5.6: terminating an instance terminates the activities still
// running in it. Work parked for a worker is such an activity: a cancel
// withdraws it, so that no worker is handed it afterwards, and says on the
// trail that it did — the row goes whether work is done or withdrawn, and
// nothing else would tell the two apart.
func TestACancelWithdrawsTheWorkParkedForWorkers(t *testing.T) {
	h := newEngineHarness(t, "Cancel Parked Work Project")
	ctx := h.Ctx()
	r := startFulfilment(t, h, "fulfilment-parked", forAWorker)
	fetched, err := h.svc.FetchAndLock(ctx, "ship", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d task(s), %v", len(fetched), err)
	}
	if err := r.cancelByMigration(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	r.requireEndedAndStill(t, entities.ProcessCancelled)

	if left := r.parked(t); len(left) != 0 {
		t.Fatalf("%d piece(s) of work are still parked for a cancelled instance", len(left))
	}
	if again, err := h.svc.FetchAndLock(ctx, "ship", "worker-2", 5, 60_000); err != nil || len(again) != 0 {
		t.Errorf("work of a cancelled instance was offered: %d task(s), %v", len(again), err)
	}
	entries, err := h.svc.GetAuditLogs(ctx, r.id)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var withdrawals []entities.AuditEntry
	for _, entry := range entries {
		if entry.Type == serviceimpl.EventParkedWorkWithdrawn {
			withdrawals = append(withdrawals, entry)
		}
	}
	if len(withdrawals) != 1 {
		t.Fatalf("the trail has %d entries for parked work withdrawn, want one for the step", len(withdrawals))
	}
	if ids, _ := withdrawals[0].Data["external_task_ids"].([]any); withdrawals[0].Data["node_id"] != "work" ||
		len(ids) != 1 || ids[0] != fetched[0].ID.String() {
		t.Errorf("the entry says %+v, want the step and the one piece of work it had parked", withdrawals[0].Data)
	}
	// The worker that had fetched it is told what anybody is told of work that
	// was withdrawn: there is no such task.
	if err := h.svc.Complete(ctx, fetched[0].ID, "worker-1", map[string]any{"shipped": true}); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("the late report was answered %v, want no such task", err)
	}
	r.requireEndedAndStill(t, entities.ProcessCancelled)
}

// An instance that holds nothing parked is cancelled as it always was, and
// its trail says nothing of work withdrawn.
func TestACancelOfAnInstanceWithNothingParkedSaysNothingOfIt(t *testing.T) {
	api, _ := countingCalls(t, respondOK)
	h := newEngineHarness(t, "Cancel Nothing Parked Project")
	r := startFulfilment(t, h, "fulfilment-nothing-parked", func() *entities.Node { return callingAPartner(api) })
	if err := r.cancelByMigration(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	r.requireEndedAndStill(t, entities.ProcessCancelled)
	entries, err := h.svc.GetAuditLogs(h.Ctx(), r.id)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type == serviceimpl.EventParkedWorkWithdrawn {
			t.Fatalf("the trail says parked work was withdrawn from an instance that had none: %+v", entry)
		}
	}
}
