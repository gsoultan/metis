package bpmn_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// An instance that has ended stays ended.
//
// Ending an instance takes its tokens and sets its status. What it had asked
// of the outside — work parked for a worker, a call queued for a partner — is
// kept in rows of its own, and those rows outlived it: a worker's report moved
// a cancelled instance on to its next step, and a queued call was made to a
// partner on behalf of an instance nobody was running any more.
//
// Two ways of ending are walked through each case. A migration's cancel, which
// is somebody deciding to end the instance; and a terminate end event reached
// on another branch, which is the process ending itself with work still out.

// fulfilment is start → fork → work (the step under test) → Send the invoice
// → end, with a second branch: Call the order off → a terminate end event.
func fulfilment(projectID uuid.UUID, key string, work *entities.Node) *entities.ProcessDefinition {
	work.ID, work.Incoming, work.Outgoing = "work", []string{"f2"}, []string{"f4"}
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key, Name: "Order fulfilment",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "fork", Type: entities.ParallelGateway, Incoming: []string{"f1"}, Outgoing: []string{"f2", "f3"}},
			work,
			{ID: "invoice", Type: entities.UserTask, Name: "Send the invoice", Incoming: []string{"f4"}, Outgoing: []string{"f5"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f5"}},
			{ID: "callOff", Type: entities.UserTask, Name: "Call the order off", Incoming: []string{"f3"}, Outgoing: []string{"f6"}},
			{ID: "stop", Type: entities.TerminateEndEvent, Incoming: []string{"f6"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "work"},
			{ID: "f3", SourceRef: "fork", TargetRef: "callOff"},
			{ID: "f4", SourceRef: "work", TargetRef: "invoice"},
			{ID: "f5", SourceRef: "invoice", TargetRef: "end"},
			{ID: "f6", SourceRef: "callOff", TargetRef: "stop"},
		},
	}
}

// forAWorker is the step under test as work parked for an outside worker.
func forAWorker() *entities.Node {
	return &entities.Node{Type: entities.ServiceTask, Name: "Ship the order", ExternalTopic: "ship"}
}

// endings is the ways an instance of fulfilment is ended with its work still
// out, the status each leaves it in, and whether it takes the work parked for
// workers with it. A cancel does. A terminate end event does not, and the
// work is removed when a worker reports on it.
var endings = []struct {
	name        string
	status      entities.ProcessStatus
	takesParked bool
	end         func(t *testing.T, r *fulfilmentRun) error
}{
	{"cancelled by a migration", entities.ProcessCancelled, true, func(_ *testing.T, r *fulfilmentRun) error { return r.cancelByMigration() }},
	{"terminated on another branch", entities.ProcessCompleted, false, func(_ *testing.T, r *fulfilmentRun) error { return r.callOff() }},
}

// fulfilmentRun is one instance of fulfilment and the versions of its process.
type fulfilmentRun struct {
	h        engineHarness
	id       uuid.UUID
	v1       uuid.UUID
	redeploy func() *entities.ProcessDefinition
}

// startFulfilment deploys fulfilment around a step and starts one instance.
func startFulfilment(t *testing.T, h engineHarness, key string, work func() *entities.Node) *fulfilmentRun {
	t.Helper()
	define := func() *entities.ProcessDefinition { return fulfilment(h.projID, key, work()) }
	v1, err := h.svc.CreateDefinition(h.Ctx(), define())
	if err != nil {
		t.Fatalf("deploy %s: %v", key, err)
	}
	id, err := h.svc.StartProcess(h.Ctx(), h.projID, key, nil)
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	return &fulfilmentRun{h: h, id: id, v1: v1, redeploy: define}
}

// cancelByMigration deploys the process again and has the migration to that
// version cancel the instances waiting at the step under test.
//
// It returns its error and never fails the test itself: one case calls it from
// a goroutine of its own, while a call to the partner is in flight.
func (r *fulfilmentRun) cancelByMigration() error {
	ctx := r.h.Ctx()
	v2, err := r.h.svc.CreateDefinition(ctx, r.redeploy())
	if err != nil {
		return fmt.Errorf("deploy the next version: %w", err)
	}
	return r.h.svc.MigrateInstances(ctx, r.v1, v2, nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"work": {Kind: servicecontracts.NodeActionCancel, Reason: "the order was withdrawn"},
		}),
		servicecontracts.WithActor("dita"))
}

// callOff completes the task on the other branch, which ends the instance at
// its terminate end event.
func (r *fulfilmentRun) callOff() error {
	ctx := r.h.Ctx()
	tasks, err := r.h.svc.ListTasks(ctx, r.h.projID)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == r.id && task.NodeID() == "callOff" && taskIsOpen(task.Status) {
			return completeAs(ctx, r.h, task, "carol", nil)
		}
	}
	return errors.New("no open task to call the order off with")
}

// requireEndedAndStill fails unless the instance has the status its ending
// left it in, holds no token, and never reached the step after the one under
// test.
func (r *fulfilmentRun) requireEndedAndStill(t *testing.T, status entities.ProcessStatus) {
	t.Helper()
	instance := requireInstanceStatus(r.h.Ctx(), t, r.h, r.id, status)
	var at []string
	for _, token := range instance.Tokens {
		if token.Node != nil {
			at = append(at, token.Node.ID)
		}
	}
	if seen := tasksEverOn(t, r.h, r.id, "invoice"); seen != 0 || len(instance.Tokens) != 0 {
		t.Fatalf("an instance that had ended (%s) moved on: it holds %d token(s) %v and has %d task(s) at the step after",
			status, len(instance.Tokens), at, seen)
	}
}

// parked is the work the instance has parked for outside workers.
func (r *fulfilmentRun) parked(t *testing.T) []*models.ExternalTaskModel {
	t.Helper()
	parked, err := r.h.repo.ExternalTask().ListByProcessInstance(r.h.Ctx(), r.id)
	if err != nil {
		t.Fatalf("read the parked work: %v", err)
	}
	return parked
}

// openIncidents is how many incidents the instance has open.
func (r *fulfilmentRun) openIncidents(t *testing.T) int {
	t.Helper()
	incidents, err := r.h.repo.Incident().ListByInstance(r.h.Ctx(), r.id)
	if err != nil {
		t.Fatalf("read the incidents: %v", err)
	}
	open := 0
	for _, incident := range incidents {
		if incident.Status == models.IncidentOpen {
			open++
		}
	}
	return open
}

// BPMN 2.0.2 §13.5.6: when a process instance is terminated, the activities
// still running in it are terminated with it, and nothing of it runs again.
//
// A worker that fetched work before its instance ended, and reports after, is
// refused: what it reports is not written, the instance is not moved, and the
// work is not offered to anybody again. It is told the work is no longer
// wanted — or, where the ending took the work with it, that there is no such
// task.
func TestAWorkerReportingOnAnInstanceThatHasEndedIsRefused(t *testing.T) {
	reports := map[string]func(h engineHarness, task uuid.UUID) error{
		"done": func(h engineHarness, task uuid.UUID) error {
			return h.svc.Complete(h.Ctx(), task, "worker-1", map[string]any{"shipped": true})
		},
		"failed, with tries left": func(h engineHarness, task uuid.UUID) error {
			return h.svc.HandleFailure(h.Ctx(), task, "worker-1", "the carrier's system is down", "", 2, 0)
		},
		"failed, giving up": func(h engineHarness, task uuid.UUID) error {
			return h.svc.HandleFailure(h.Ctx(), task, "worker-1", "the carrier's system is down", "", 0, 0)
		},
	}
	for _, ending := range endings {
		for how, report := range reports {
			t.Run(ending.name+", reported "+how, func(t *testing.T) {
				h := newEngineHarness(t, "Ended Instance Worker Project")
				ctx := h.Ctx()
				r := startFulfilment(t, h, "fulfilment-worker", forAWorker)
				fetched, err := h.svc.FetchAndLock(ctx, "ship", "worker-1", 1, 60_000)
				if err != nil || len(fetched) != 1 {
					t.Fatalf("fetch: %d task(s), %v", len(fetched), err)
				}
				if err := ending.end(t, r); err != nil {
					t.Fatalf("end the instance: %v", err)
				}
				r.requireEndedAndStill(t, ending.status)

				err = report(h, fetched[0].ID)
				want := fmt.Sprintf("This work belongs to an instance that has ended (%s); it is no longer wanted.", ending.status)
				switch {
				case ending.takesParked:
					// The work went with the instance, and the worker hears what
					// anybody hears of work that was withdrawn.
					if !errors.Is(err, apierr.ErrNotFound) {
						t.Errorf("the report was answered %v, want no such task: the cancel withdrew it", err)
					}
				case !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), want):
					t.Errorf("the report was answered %v\nwant it refused as something the worker can read: %s", err, want)
				}
				r.requireEndedAndStill(t, ending.status)
				instance, err := h.engine.GetInstance(ctx, r.id)
				if err != nil {
					t.Fatalf("read the instance: %v", err)
				}
				if _, written := instance.Variables["shipped"]; written {
					t.Error("what the worker reported was written on an instance that had ended")
				}
				if open := r.openIncidents(t); open != 0 {
					t.Errorf("a worker's failure raised %d incident(s) on an instance that had ended", open)
				}
				if left := r.parked(t); len(left) != 0 {
					t.Errorf("%d piece(s) of work are still parked for an instance that has ended", len(left))
				}
				if again, err := h.svc.FetchAndLock(ctx, "ship", "worker-2", 5, 60_000); err != nil || len(again) != 0 {
					t.Errorf("work of an instance that has ended was offered again: %d task(s), %v", len(again), err)
				}
			})
		}
	}
}

// Nothing looks at an instance when its parked work is handed out, so work of
// an instance that has ended can still be fetched. The worker that fetches it
// is refused when it reports, as above, and the work is gone after that.
func TestWorkFetchedAfterItsInstanceEndedIsRefusedAndRemoved(t *testing.T) {
	h := newEngineHarness(t, "Ended Instance Late Fetch Project")
	ctx := h.Ctx()
	r := startFulfilment(t, h, "fulfilment-late-fetch", forAWorker)
	if err := r.callOff(); err != nil {
		t.Fatalf("end the instance: %v", err)
	}
	r.requireEndedAndStill(t, entities.ProcessCompleted)
	if left := r.parked(t); len(left) != 1 {
		t.Fatalf("%d piece(s) of work are parked after the instance ended; this test needs the one it left", len(left))
	}

	fetched, err := h.svc.FetchAndLock(ctx, "ship", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d task(s), %v", len(fetched), err)
	}
	if err := h.svc.Complete(ctx, fetched[0].ID, "worker-1", nil); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("the report was answered %v, want it refused", err)
	}
	r.requireEndedAndStill(t, entities.ProcessCompleted)
	if left := r.parked(t); len(left) != 0 {
		t.Errorf("%d piece(s) of work are still parked for an instance that has ended", len(left))
	}
	// Told again, the worker hears what anybody hears of work that is gone.
	if err := h.svc.Complete(ctx, fetched[0].ID, "worker-1", nil); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a second report on work that was removed was answered %v, want no such task", err)
	}
}

// A worker whose instance is still running reports as it always did: the
// refusal is of an ended instance, not of a late report.
func TestAWorkerReportingOnARunningInstanceMovesItOn(t *testing.T) {
	h := newEngineHarness(t, "Running Instance Worker Project")
	ctx := h.Ctx()
	r := startFulfilment(t, h, "fulfilment-running", forAWorker)
	fetched, err := h.svc.FetchAndLock(ctx, "ship", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d task(s), %v", len(fetched), err)
	}
	if err := h.svc.Complete(ctx, fetched[0].ID, "worker-1", map[string]any{"shipped": true}); err != nil {
		t.Fatalf("report: %v", err)
	}
	requireInstanceStatus(ctx, t, h, r.id, entities.ProcessActive)
	if seen := tasksEverOn(t, h, r.id, "invoice"); seen != 1 {
		t.Fatalf("the instance has %d task(s) at the step after the work, want one", seen)
	}
}
