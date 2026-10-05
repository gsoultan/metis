package bpmn_test

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// What a cancel in place takes with the instance, and what it says of it.
//
// The effect is the one a migration's cancel has (cancelled_instance_test.go
// walks it through a migration). These walk it through the in-place command,
// and pin what only that command writes: the counts on its ledger row, the
// incidents the row names, and the warning in the plan.

// theCancelOf is the one cancel an instance's ledger records.
func (w waiver) theCancelOf(t *testing.T, instanceID uuid.UUID) entities.Deviation {
	t.Helper()
	var cancels []entities.Deviation
	for _, row := range w.ledger(t, instanceID) {
		if row.Kind == entities.DeviationCancel {
			cancels = append(cancels, row)
		}
	}
	if len(cancels) != 1 {
		t.Fatalf("the ledger records %d cancel(s), want exactly one", len(cancels))
	}
	return cancels[0]
}

// BPMN 2.0.2 §13.5.6. A cancelled instance stays cancelled when an outside
// worker reports: the work the instance had parked went with it, so the worker
// is refused, and nothing it sends moves the instance on.
func TestCancellingInPlaceTakesTheWorkParkedForWorkers(t *testing.T) {
	h := newEngineHarness(t, "In Place Parked Work Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	r := startFulfilment(t, h, "in-place-parked", forAWorker)
	fetched, err := h.svc.FetchAndLock(ctx, "ship", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d task(s), %v", len(fetched), err)
	}

	cancel := deviationCommand(entities.DeviationCancel, r.id, "work", nil)
	// An administrator is shown everything the act would take, before it is.
	if plan, want := w.preview(t, cancel), "1 piece(s) of work parked for outside workers will be withdrawn."; !plan.Applicable() || !said(plan.Warnings, want) {
		t.Fatalf("the plan: refusals:%s\nwarnings:%s\nwant the warning\n  %s", lines(plan.Refusals), lines(plan.Warnings), want)
	}
	w.mustApply(t, cancel)
	r.requireEndedAndStill(t, entities.ProcessCancelled)
	if left := r.parked(t); len(left) != 0 {
		t.Errorf("%d piece(s) of work are still parked for a cancelled instance", len(left))
	}
	if again, err := h.svc.FetchAndLock(ctx, "ship", "worker-2", 5, 60_000); err != nil || len(again) != 0 {
		t.Errorf("work of a cancelled instance was offered: %d task(s), %v", len(again), err)
	}

	// The worker that had fetched it reports.
	if err := h.svc.Complete(ctx, fetched[0].ID, "worker-1", map[string]any{"shipped": true}); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("the worker's report was answered %v, want it refused: there is no such task", err)
	}
	r.requireEndedAndStill(t, entities.ProcessCancelled)

	row := w.theCancelOf(t, r.id)
	wantDetails := map[string]any{"withdrawn": float64(1), "tasks_listed": float64(1), "external_tasks_withdrawn": float64(1), "incidents_closed": float64(0)}
	if !reflect.DeepEqual(row.Details, wantDetails) {
		t.Errorf("the row's details: %v, want %v", row.Details, wantDetails)
	}
	if entry := theEntryOf(t, h, r.id, serviceimpl.EventInstanceCancelled); entry.Data["external_tasks_withdrawn"] != float64(1) || entry.Data["incidents_closed"] != float64(0) {
		t.Errorf("the entry's data: %+v, want it to count the parked work withdrawn, and no incident closed", entry.Data)
	}
	if entries := entriesOfType(t, h, r.id, serviceimpl.EventParkedWorkWithdrawn); len(entries) != 1 || entries[0].Data["node_id"] != "work" {
		t.Errorf("the trail's entries for parked work withdrawn: %+v, want one for the step", entries)
	}
}

// No call is made to a partner for an instance cancelled in place: the call
// queued for the step it was cancelled at is settled without being made.
func TestNoCallIsMadeForAnInstanceCancelledInPlace(t *testing.T) {
	api, calls := countingCalls(t, respondOK)
	h := newEngineHarness(t, "In Place Queued Call Project")
	w := newWaiver(h)
	r := startFulfilment(t, h, "in-place-call", func() *entities.Node { return callingAPartner(api) })

	w.mustApply(t, deviationCommand(entities.DeviationCancel, r.id, "work", nil))
	r.runDueJobs(t)
	if made := calls.Load(); made != 0 {
		t.Errorf("%d call(s) were made for an instance that had been cancelled", made)
	}
	if settled := r.theCall(t); settled.Status != models.JobCompleted {
		t.Errorf("the call's job is %s, want it settled so that nothing runs it again", settled.Status)
	}
	if open := r.openIncidents(t); open != 0 {
		t.Errorf("%d incident(s) were raised on a cancelled instance", open)
	}
	r.requireEndedAndStill(t, entities.ProcessCancelled)
}

// A cancel closes the incidents the instance has open, says so before it is
// applied, and records which: the row names them, as they were and as they
// are, and counts them; the trail entry counts them.
func TestCancellingInPlaceClosesTheIncidentsOnTheInstance(t *testing.T) {
	t.Run("an instance held in place", func(t *testing.T) {
		h := newEngineHarness(t, "In Place Hold Then Cancel Project")
		h.recordsAsProductionDoes()
		w := newWaiver(h)
		r := startFulfilment(t, h, "in-place-held", forAWorker)
		w.mustApply(t, deviationCommand(entities.DeviationHold, r.id, "work", nil))
		incident := r.theOpenIncident(t)
		attention, err := h.engine.InstanceAttention(h.Ctx(), h.projID, repocontracts.InstanceFilter{}, []uuid.UUID{r.id})
		if err != nil || attention.Total != 1 {
			t.Fatalf("a held instance: %d instance(s) need attention (err %v); this test needs the one", attention.Total, err)
		}

		cancel := deviationCommand(entities.DeviationCancel, r.id, "work", nil)
		plan := w.preview(t, cancel)
		want := []string{
			"1 piece(s) of work parked for outside workers will be withdrawn.",
			"1 open incident(s) on this instance will be closed.",
		}
		if !plan.Applicable() || !reflect.DeepEqual(plan.Warnings, want) {
			t.Fatalf("the plan: refusals:%s\nwarnings:%s\nwant the warnings:%s", lines(plan.Refusals), lines(plan.Warnings), lines(want))
		}
		w.mustApply(t, cancel)
		r.requireEndedAndStill(t, entities.ProcessCancelled)

		r.requireClosed(t, incident)
		r.requireNoAttention(t)
		row := w.theCancelOf(t, r.id)
		was, is := map[string]any{incident.ID.String(): map[string]any{"status": "open"}}, map[string]any{incident.ID.String(): map[string]any{"status": "resolved"}}
		if !reflect.DeepEqual(row.Before["incidents"], any(was)) || !reflect.DeepEqual(row.After["incidents"], any(is)) {
			t.Errorf("the row says of the incidents: before %v, after %v\nwant %v and %v", row.Before["incidents"], row.After["incidents"], was, is)
		}
		if row.Details["incidents_closed"] != float64(1) || row.Details["external_tasks_withdrawn"] != float64(1) {
			t.Errorf("the row's details: %v, want one incident closed and one piece of parked work withdrawn", row.Details)
		}
		if entry := theEntryOf(t, h, r.id, serviceimpl.EventInstanceCancelled); entry.Data["incidents_closed"] != float64(1) {
			t.Errorf("the entry's data: %+v, want it to count the incident closed", entry.Data)
		}
	})

	t.Run("an instance whose call failed for good", func(t *testing.T) {
		api, calls := countingCalls(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the carrier's system is down", http.StatusInternalServerError)
		})
		h := newEngineHarness(t, "In Place Failed Call Then Cancel Project")
		w := newWaiver(h)
		r := startFulfilment(t, h, "in-place-failed-call", func() *entities.Node { return callingAPartner(api) })
		for range 3 {
			r.runDueJobs(t)
		}
		incident := r.theOpenIncident(t)

		w.mustApply(t, deviationCommand(entities.DeviationCancel, r.id, "work", nil))
		r.requireEndedAndStill(t, entities.ProcessCancelled)
		r.requireClosed(t, incident)
		r.requireNoAttention(t)

		// Closed, and not to be tried again: resolving it now does nothing, and
		// no call is made.
		if err := h.svc.ResolveIncident(h.Ctx(), incident.ID); err != nil {
			t.Fatalf("resolve the closed incident: %v", err)
		}
		r.runDueJobs(t)
		if job := r.theCall(t); job.Status != models.JobFailed || calls.Load() != 3 {
			t.Errorf("after the cancel the call's job is %s and %d call(s) were made in all, want it left failed and the three made before", job.Status, calls.Load())
		}
		if row := w.theCancelOf(t, r.id); row.Details["incidents_closed"] != float64(1) {
			t.Errorf("the row's details: %v, want one incident closed", row.Details)
		}
	})

	t.Run("an instance with none open says nothing of it", func(t *testing.T) {
		h := newEngineHarness(t, "In Place No Incident Project")
		w := newWaiver(h)
		id := w.start(t, opsApproval(h.projID, "in-place-no-incident"), nil)
		cancel := deviationCommand(entities.DeviationCancel, id, "opsApprove", nil)
		for _, warning := range w.preview(t, cancel).Warnings {
			if strings.Contains(warning, "incident") || strings.Contains(warning, "parked") {
				t.Errorf("the plan warns of incidents or parked work on an instance that has neither: %s", warning)
			}
		}
		w.mustApply(t, cancel)
		row := w.theCancelOf(t, id)
		if _, named := row.After["incidents"]; named || row.Details["incidents_closed"] != float64(0) {
			t.Errorf("the row: after %v, details %v; want no incidents named and none counted", row.After, row.Details)
		}
	})
}

// A cancel does not delete a pending timer: a job has no delete. Each timer of
// a cancelled instance comes due all the same — a deadline on the step it was
// cancelled at, and a wait on another branch — finds that the instance is not
// waiting for it, and does nothing: the instance stays cancelled and holds no
// token, no task opens where the deadline leads or after the wait, and no
// incident is raised. The jobs are settled, and nothing else is written.
func TestATimerDueAfterACancelInPlaceMovesNothingAndRaisesNoIncident(t *testing.T) {
	h := newEngineHarness(t, "Cancel Then Timers Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "cancel-then-timers",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita"},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "review", Properties: map[string]any{"timer_duration": "P7D"}},
			{ID: "chase", Type: entities.UserTask, Name: "Chase the reviewer"},
			{ID: "wait", Type: entities.IntermediateCatchEvent, Name: "Wait for the cooling-off period", Properties: map[string]any{"timer_duration": "P14D"}},
			{ID: "pay", Type: entities.UserTask, Name: "Pay the claim"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "review"},
			{ID: "f3", SourceRef: "fork", TargetRef: "wait"},
			{ID: "f4", SourceRef: "review", TargetRef: "end"},
			{ID: "f5", SourceRef: "deadline", TargetRef: "chase"},
			{ID: "f6", SourceRef: "chase", TargetRef: "end"},
			{ID: "f7", SourceRef: "wait", TargetRef: "pay"},
			{ID: "f8", SourceRef: "pay", TargetRef: "end"},
		},
	}, nil)
	if tokensOn(t, h, id, "review") != 1 || tokensOn(t, h, id, "wait") != 1 {
		t.Fatal("the instance is not waiting at the review and at the wait; this test needs both")
	}

	w.mustApply(t, deviationCommand(entities.DeviationCancel, id, "review", nil))
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)

	// The week and the fortnight pass.
	if moved := h.dueNow(ctx, t, id); moved != 2 {
		t.Fatalf("%d timer(s) were waiting to come due on the cancelled instance, want the deadline and the wait", moved)
	}
	due := everyRow(t, h)
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if now := requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled); len(now.Tokens) != 0 {
		t.Fatalf("a timer of a cancelled instance left it holding %d token(s)", len(now.Tokens))
	}
	for _, step := range []string{"review", "chase", "pay"} {
		if open := h.openTasksOn(t, id, step); open != 0 {
			t.Errorf("%d task(s) are open on %q after the timers of a cancelled instance came due", open, step)
		}
	}
	if incidents, err := h.svc.ListIncidents(ctx, id); err != nil || len(incidents) != 0 {
		t.Errorf("%d incident(s) on the cancelled instance after its timers came due (err %v), want none", len(incidents), err)
	}
	jobs, err := h.repo.Job().ListByInstance(ctx, id)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("the instance's jobs: %d (err %v), want the two timers and no other", len(jobs), err)
	}
	for _, job := range jobs {
		if job.Status != models.JobCompleted {
			t.Errorf("the timer on %q is %s after it came due, want it settled", job.NodeID, job.Status)
		}
	}
	// Settling the two jobs is all that was written.
	for _, changed := range tablesThatDiffer(due, everyRow(t, h)) {
		if !strings.HasPrefix(changed, "jobs ") {
			t.Errorf("a timer of a cancelled instance coming due changed %s", changed)
		}
	}
}
