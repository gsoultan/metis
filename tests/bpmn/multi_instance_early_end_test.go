package bpmn_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// A step that ends before all of its runs have finished, when those runs are
// not approvals in somebody's inbox: work parked for an outside worker, and
// processes the step called.
//
// Ending the step has to leave nothing behind that will be refused for ever.

// parkedFor counts the external tasks the instance has parked on nodeID, in
// the database.
func parkedFor(t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	var count int64
	if err := h.db.Raw(`SELECT count(*) FROM external_tasks
		 WHERE instance_id = ? AND node_id = ? AND deleted_at IS NULL`,
		instanceID, nodeID).Scan(&count).Error; err != nil {
		t.Fatalf("count external tasks: %v", err)
	}
	return int(count)
}

// calledBy returns the ids of the processes instanceID has called, oldest
// first.
func calledBy(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID) []uuid.UUID {
	t.Helper()
	children, err := h.repo.Process().ListByParent(ctx, instanceID)
	if err != nil {
		t.Fatalf("list the called processes: %v", err)
	}
	ids := make([]uuid.UUID, 0, len(children))
	for _, child := range children {
		ids = append(ids, uuid.UUID(child.ID))
	}
	return ids
}

// deploySupplierCheck deploys the process the call activities below call: one
// review, which records whether the supplier was approved.
func deploySupplierCheck(t *testing.T, h engineHarness) {
	t.Helper()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "supplier-check",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Compliance review",
				Properties: testutils.FormDeclaring("approved")},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "cf1", SourceRef: "start", TargetRef: "review"},
			{ID: "cf2", SourceRef: "review", TargetRef: "end"},
		},
	})
}

// finishCalled completes the one review a called process is waiting on.
func finishCalled(ctx context.Context, t *testing.T, h engineHarness, childID uuid.UUID, approved bool) error {
	t.Helper()
	reviews := openIterationTasks(ctx, t, h, childID, "review")
	if len(reviews) != 1 {
		t.Fatalf("the called process is waiting on %d review(s), want 1", len(reviews))
	}
	return completeAs(ctx, h, reviews[0], "carol", map[string]any{"approved": approved})
}

// requireLateReturnRecorded fails unless the instance's trail says a process
// it called finished after the step that called it had ended, in words that
// name no id.
func requireLateReturnRecorded(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, stepName string) {
	t.Helper()
	trail, err := h.engine.GetAuditLogs(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range trail {
		if entry.Type != "called_process_finished_late" {
			continue
		}
		if !strings.Contains(entry.Narrative, stepName) || !strings.Contains(entry.Narrative, "had already ended") {
			t.Fatalf("the trail's line does not say what happened to which step: %q", entry.Narrative)
		}
		if strings.Contains(entry.Narrative, instanceID.String()) {
			t.Fatalf("the trail's line names an id a person cannot read: %q", entry.Narrative)
		}
		return
	}
	t.Fatal("the trail does not say a called process finished after its step had ended")
}

// BPMN 2.0.2 §10.3.8: a completionCondition that holds cancels the remaining
// activity instances.
//
// Work parked for a worker is a row of its own and outlived the step. The
// worker went on being offered the third check; completing it was refused,
// because the step had finished, and rolled back — so its lock ran out and it
// was offered again, and refused again, for ever.
func TestAnEarlyEndWithdrawsTheWorkParkedForWorkers(t *testing.T) {
	h := newEngineHarness(t, "Parked Work Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "two-of-three-checks",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.ServiceTask, Name: "Check the supplier", ExternalTopic: "supplier-check",
				MultiInstanceType: "parallel", Collection: "suppliers", ElementVariable: "supplier",
				CompletionCondition: "nrOfCompletedInstances >= 2"},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	})
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "two-of-three-checks", map[string]any{
		"suppliers": []any{"northwind", "contoso", "fabrikam"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if parked := parkedFor(t, h, instanceID, "check"); parked != 3 {
		t.Fatalf("three checks were asked for and %d are parked", parked)
	}

	fetched, err := h.svc.FetchAndLock(ctx, "supplier-check", "worker", 2, 60_000)
	if err != nil || len(fetched) != 2 {
		t.Fatalf("fetch two checks: %d, %v", len(fetched), err)
	}
	for _, task := range fetched {
		if err := h.svc.Complete(ctx, task.ID, "worker", nil); err != nil {
			t.Fatalf("complete a check: %v", err)
		}
	}

	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("after the second check the process moved on %d times, want once", seen)
	}
	offered, err := h.svc.FetchAndLock(ctx, "supplier-check", "another-worker", 10, 60_000)
	if err != nil {
		t.Fatalf("fetch again: %v", err)
	}
	if len(offered) != 0 {
		t.Fatalf("a worker is still offered %d check(s) for a step that ended", len(offered))
	}
	if parked := parkedFor(t, h, instanceID, "check"); parked != 0 {
		t.Fatalf("%d check(s) are still parked for a step that ended", parked)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "check"); len(left) != 0 {
		t.Fatalf("the finished step still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §13.4.3 (Intermediate Boundary Events): an interrupting event
// ends the activity it is attached to — the work a worker was asked for
// included.
//
// The deadline took the token and left the row. A worker fetched the check
// afterwards, did it, and reported back to a step the process had abandoned.
func TestADeadlineWithdrawsTheWorkParkedForAWorker(t *testing.T) {
	h := newEngineHarness(t, "Parked Deadline Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "check-by-friday",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.ServiceTask, Name: "Check the supplier", ExternalTopic: "supplier-check"},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "check", Properties: map[string]any{
				"timer_duration": "P7D",
			}},
			{ID: "chase", Type: entities.UserTask, Name: "Chase the check"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "end"},
			{ID: "f3", SourceRef: "deadline", TargetRef: "chase"},
		},
	})
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "check-by-friday", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if parked := parkedFor(t, h, instanceID, "check"); parked != 1 {
		t.Fatalf("one check was asked for and %d are parked", parked)
	}

	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Fatal("no deadline was waiting to come due")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	if !h.waitingAt(ctx, t, instanceID, "chase") {
		t.Fatal("the deadline did not take the process on to chasing")
	}

	offered, err := h.svc.FetchAndLock(ctx, "supplier-check", "worker", 10, 60_000)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(offered) != 0 {
		t.Fatalf("a worker is still offered %d check(s) for a step its deadline ended", len(offered))
	}
}

// BPMN 2.0.2 §10.3.8: a completionCondition that holds cancels the remaining
// activity instances and produces a token — one.
//
// The engine has no way to end a process from outside it, so the third called
// process runs on. When it finished, its last step failed: handing back to a
// step that had already ended was refused, the refusal rolled the completion
// back, and the called process could never end.
func TestACalledProcessReturningToAStepThatEndedEarlyEndsWithoutMovingItsParent(t *testing.T) {
	h := newEngineHarness(t, "Called Twice Of Three Project")
	ctx := h.Ctx()
	deploySupplierCheck(t, h)
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "two-of-three-suppliers",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier",
				MultiInstanceType: "parallel", Collection: "suppliers", ElementVariable: "supplier",
				CompletionCondition: "nrOfCompletedInstances >= 2",
				Properties:          map[string]any{"called_process_key": "supplier-check"}},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	})
	parentID, err := h.svc.StartProcess(ctx, h.projID, "two-of-three-suppliers", map[string]any{
		"suppliers": []any{"northwind", "contoso", "fabrikam"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	called := calledBy(ctx, t, h, parentID)
	if len(called) != 3 {
		t.Fatalf("three suppliers were to be checked and %d process(es) were called", len(called))
	}

	for _, childID := range called[:2] {
		if err := finishCalled(ctx, t, h, childID, true); err != nil {
			t.Fatalf("finish a called process: %v", err)
		}
	}
	if seen := tasksEverOn(t, h, parentID, "record"); seen != 1 {
		t.Fatalf("after the second check the process moved on %d times, want once", seen)
	}
	if left := tokenIterationsOn(ctx, t, h, parentID, "check"); len(left) != 0 {
		t.Fatalf("the finished step still holds tokens %v", left)
	}

	// The third finishes after the step that called it has ended.
	late := called[2]
	if err := finishCalled(ctx, t, h, late, false); err != nil {
		t.Fatalf("the called process could not end: %v", err)
	}
	requireInstanceStatus(ctx, t, h, late, entities.ProcessCompleted)
	if seen := tasksEverOn(t, h, parentID, "record"); seen != 1 {
		t.Fatalf("a called process finishing late moved its parent on again: %d times", seen)
	}
	parent, err := h.engine.GetInstance(ctx, parentID)
	if err != nil {
		t.Fatalf("reload the parent: %v", err)
	}
	if parent.Variables["approved"] != true {
		t.Errorf("the late result was written over the parent's: approved = %v", parent.Variables["approved"])
	}
	requireLateReturnRecorded(ctx, t, h, parentID, "Check the supplier")
	finishRecording(ctx, t, h, parentID)
}

// BPMN 2.0.2 §13.4.3: an interrupting boundary event ends the activity it is
// attached to, and the token leaves by the event's flow — not also by the
// activity's.
//
// The deadline took the call activity's token and left the called process
// running. When that finished it resumed the parent at a step holding no
// token, removing nothing and following the step's outgoing flow: the process
// went on to chasing and, later, to signing as well.
func TestACalledProcessReturningAfterItsDeadlineDoesNotMoveItsParent(t *testing.T) {
	h := newEngineHarness(t, "Called Past Deadline Project")
	ctx := h.Ctx()
	deploySupplierCheck(t, h)
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "check-then-sign",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier",
				Properties: map[string]any{"called_process_key": "supplier-check"}},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "check", Properties: map[string]any{
				"timer_duration": "P7D",
			}},
			{ID: "chase", Type: entities.UserTask, Name: "Chase the check"},
			{ID: "sign", Type: entities.UserTask, Name: "Sign the contract"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "sign"},
			{ID: "f3", SourceRef: "sign", TargetRef: "end"},
			{ID: "f4", SourceRef: "deadline", TargetRef: "chase"},
		},
	})
	parentID, err := h.svc.StartProcess(ctx, h.projID, "check-then-sign", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	called := calledBy(ctx, t, h, parentID)
	if len(called) != 1 {
		t.Fatalf("%d process(es) were called, want 1", len(called))
	}

	if moved := h.dueNow(ctx, t, parentID); moved == 0 {
		t.Fatal("no deadline was waiting to come due")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	if !h.waitingAt(ctx, t, parentID, "chase") {
		t.Fatal("the deadline did not take the process on to chasing")
	}

	if err := finishCalled(ctx, t, h, called[0], true); err != nil {
		t.Fatalf("the called process could not end: %v", err)
	}
	requireInstanceStatus(ctx, t, h, called[0], entities.ProcessCompleted)
	if seen := tasksEverOn(t, h, parentID, "sign"); seen != 0 {
		t.Fatalf("a called process finishing after the deadline took its parent on to signing %d time(s)", seen)
	}
	requireLateReturnRecorded(ctx, t, h, parentID, "Check the supplier")
}

// BPMN 2.0.2 §10.3.8: once the completionCondition holds the remaining
// instances are cancelled, so none of them can complete afterwards.
//
// Whatever still reports a run of a finished step straight to the engine —
// naming the run it was, or naming none — is refused in words a person can
// read, and nothing moves.
func TestALateCompletionForAFinishedStepIsRefusedAndChangesNothing(t *testing.T) {
	h := newEngineHarness(t, "Late Completion Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h,
		approvalDefinition(h.projID, "late-two-of-three", "parallel", "nrOfCompletedInstances >= 2"),
		"ana", "budi", "citra")
	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}
	for _, task := range open[:2] {
		if err := completeAs(ctx, h, task, "carol", nil); err != nil {
			t.Fatalf("approve: %v", err)
		}
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("after the second approval the process moved on %d times, want once", seen)
	}

	for _, iteration := range []string{"2", ""} {
		instance, err := h.engine.GetInstance(ctx, instanceID)
		if err != nil {
			t.Fatalf("reload instance: %v", err)
		}
		def, err := h.engine.GetProcessDefinition(ctx, instance.Definition.ID)
		if err != nil {
			t.Fatalf("load definition: %v", err)
		}

		err = h.engine.ProceedIteration(ctx, &instance, def, "approve", iteration)
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("a completion for run %q of a finished step got %v, want a refusal", iteration, err)
		}
		if text := err.Error(); !strings.Contains(text, "already finished") ||
			strings.Contains(text, "approve") || strings.Contains(text, instanceID.String()) {
			t.Fatalf("the refusal does not say the step has finished in words a person can read: %q", text)
		}

		if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
			t.Fatalf("the refused completion moved the process on again: %d times", seen)
		}
		if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != 0 {
			t.Fatalf("the refused completion put tokens back on the step: %v", left)
		}
		if !h.waitingAt(ctx, t, instanceID, "record") {
			t.Fatal("the refused completion took the process off the step it was waiting on")
		}
	}
	finishRecording(ctx, t, h, instanceID)
}
