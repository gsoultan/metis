package instancemigration

import (
	"context"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	observercontracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// Skipping a step takes work out of somebody's hands.
//
// The trail records why, which answers the auditor's question and not the
// assignee's: from their side the task simply disappears, which looks the same
// as a colleague completing it, or as a bug.

// withdrawalWatcher records the task-cancelled events the engine raises, which
// is what the notification observer turns into somebody being told.
type withdrawalWatcher struct {
	events []entities.ProcessEvent
}

func (w *withdrawalWatcher) OnEvent(_ context.Context, event entities.ProcessEvent) {
	if event.Type == entities.EventTaskCanceled {
		w.events = append(w.events, event)
	}
}

var _ observercontracts.ProcessObserver = (*withdrawalWatcher)(nil)

func TestSkippingAStepAnnouncesItToWhoeverHeldIt(t *testing.T) {
	f := newFixture(t)
	watcher := &withdrawalWatcher{}
	f.dispatcher.Register(watcher)

	v1, v2 := f.parkedOnOpsApprove(t)

	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "the role was eliminated"},
		}),
		servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(watcher.events) != 1 {
		t.Fatalf("skipping a step somebody was holding raised %d withdrawal events, want 1", len(watcher.events))
	}
	event := watcher.events[0]
	if event.Assignee != "ollie" {
		t.Errorf("the withdrawal names %q; ollie was holding the operations approval", event.Assignee)
	}
	if event.Node == nil || event.Node.ID != "opsApprove" {
		t.Errorf("the withdrawal does not name the step it took away: %+v", event.Node)
	}
	if event.Instance == nil {
		t.Error("the withdrawal carries no instance, so nothing can link it to the work")
	}
}

func TestCancellingAnInstanceAnnouncesTheWorkItTakesAway(t *testing.T) {
	f := newFixture(t)
	watcher := &withdrawalWatcher{}
	f.dispatcher.Register(watcher)

	v1, v2 := f.parkedOnOpsApprove(t)

	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionCancel, Reason: "re-quote"},
		}),
		servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(watcher.events) != 1 {
		t.Fatalf("cancelling an instance with work in somebody's hands raised %d withdrawal events, want 1",
			len(watcher.events))
	}
	if got := watcher.events[0].Assignee; got != "ollie" {
		t.Errorf("the withdrawal names %q; ollie was holding the operations approval", got)
	}
}
