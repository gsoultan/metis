package bpmn_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// forgetIterations puts the instance's tasks back the way a release before
// migration 31 wrote them: no iteration recorded.
func forgetIterations(t *testing.T, h engineHarness, instanceID uuid.UUID) {
	t.Helper()
	if err := h.db.Exec(`UPDATE tasks SET iteration_id = NULL WHERE instance_id = ?`, instanceID).Error; err != nil {
		t.Fatalf("forget the iterations: %v", err)
	}
}

// finishRecording completes the step after the approval and expects the
// instance to end with nothing left on it.
func finishRecording(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID) {
	t.Helper()
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("the process moved past the approval %d times, want exactly once", seen)
	}
	completeTaskAt(ctx, t, h, instanceID, "record", nil)
	instance := requireInstanceStatus(ctx, t, h, instanceID, entities.ProcessCompleted)
	if len(instance.Tokens) != 0 {
		t.Fatalf("the finished instance still holds %d token(s)", len(instance.Tokens))
	}
	for name := range instance.Variables {
		if strings.HasPrefix(name, "_mi_") || strings.HasPrefix(name, "nrOf") {
			t.Errorf("engine bookkeeping %q is in the business variables", name)
		}
	}
}

// BPMN 2.0.2 §13.2.7 (Multiple Instances Activity): the activity completes when
// all of its instances have completed, and a token then leaves it — one token,
// and none stays behind.
//
// CompleteTask proceeded with no iteration, RemoveTokenByIteration(node, "")
// removed nothing, and the three iteration tokens stayed on the approval for
// good: the process moved on and its end event never found the instance empty.
func TestAParallelApprovalCompletedThroughTheInboxFinishesItsInstance(t *testing.T) {
	h := newEngineHarness(t, "Parallel Approval Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h, approvalDefinition(h.projID, "purchase-approval", "parallel", ""),
		"ana", "budi", "citra")

	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}
	for done, task := range open {
		if err := completeAs(ctx, h, task, "carol", nil); err != nil {
			t.Fatalf("complete approval %d: %v", done, err)
		}
		if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != len(open)-done-1 {
			t.Fatalf("after %d of 3 approvals the step still holds tokens %v", done+1, left)
		}
	}

	if !h.waitingAt(ctx, t, instanceID, "record") {
		t.Fatal("every approver answered and the process did not move on")
	}
	recording := openIterationTasks(ctx, t, h, instanceID, "record")
	if len(recording) != 1 {
		t.Fatalf("a step that runs once opened %d task(s)", len(recording))
	}
	if recording[0].IterationID != "" {
		t.Fatalf("a step that runs once records iteration %q, want none", recording[0].IterationID)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §10.3.8 (Loop Characteristics, isSequential) and §13.2.7: the
// instances run one after another, each started when the one before completes.
func TestASequentialApprovalAsksOnePersonAtATimeAndFinishes(t *testing.T) {
	h := newEngineHarness(t, "Sequential Approval Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h, approvalDefinition(h.projID, "purchase-approval-in-turn", "sequential", ""),
		"ana", "budi", "citra")

	for _, iteration := range []string{"0", "1", "2"} {
		open := openIterationTasks(ctx, t, h, instanceID, "approve")
		if len(open) != 1 {
			t.Fatalf("at iteration %s, %d approval(s) are open; one at a time means one", iteration, len(open))
		}
		if open[0].IterationID != iteration {
			t.Fatalf("the open approval is iteration %q, want %s", open[0].IterationID, iteration)
		}
		if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
			t.Fatalf("complete iteration %s: %v", iteration, err)
		}
	}

	if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != 0 {
		t.Fatalf("the finished approval still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §13.2.7: one completed instance, one token retired.
//
// A task created before migration 31 records no iteration. The rule: it
// retires the lowest-numbered iteration still waiting on its step — exactly
// one, and the same one every time.
func TestATaskFromBeforeMigration31RetiresTheLowestIterationStillWaiting(t *testing.T) {
	h := newEngineHarness(t, "Legacy Approval Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h, approvalDefinition(h.projID, "purchase-approval-legacy", "parallel", ""),
		"ana", "budi", "citra")
	forgetIterations(t, h, instanceID)

	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("staging failed: %d approvals open, want 3", len(open))
	}
	if open[0].IterationID != "" {
		t.Fatalf("staging failed: the task still records iteration %q", open[0].IterationID)
	}
	if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
		t.Fatalf("complete a task with no iteration: %v", err)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); !slices.Equal(left, []string{"1", "2"}) {
		t.Fatalf("one approval retired tokens down to %v, want [1 2]", left)
	}

	for _, task := range open[1:] {
		if err := completeAs(ctx, h, task, "carol", nil); err != nil {
			t.Fatalf("complete a task with no iteration: %v", err)
		}
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != 0 {
		t.Fatalf("the finished approval still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §13.2.7: the activity produces one token when it completes.
//
// An approval one person had already given before the upgrade: the count says
// one of three, and — because the old completion retired nothing — all three
// tokens are still on the step. The two approvals left retire two; the third
// token has no task behind it and has to go when the step finishes, or the
// instance can never end.
func TestAnApprovalHalfDoneBeforeTheUpgradeStillAdvancesOnce(t *testing.T) {
	h := newEngineHarness(t, "Half Done Approval Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h, approvalDefinition(h.projID, "purchase-approval-half-done", "parallel", ""),
		"ana", "budi", "citra")
	forgetIterations(t, h, instanceID)
	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("staging failed: %d approvals open, want 3", len(open))
	}

	// What the release before did when the first approver completed: the task
	// closed, the count went up, and no token came off.
	if err := h.repo.Task().UpdateStatus(ctx, open[0].ID, models.TaskCompleted); err != nil {
		t.Fatalf("close the first approval the old way: %v", err)
	}
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	instance.CompleteMultiInstanceIteration("approve")
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("stage the old count: %v", err)
	}

	for _, task := range open[1:] {
		if err := completeAs(ctx, h, task, "carol", nil); err != nil {
			t.Fatalf("complete an approval after the upgrade: %v", err)
		}
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != 0 {
		t.Fatalf("the finished approval still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §13.2.7: each instance is counted once.
//
// The engine counted whatever it was told had finished. An iteration reported
// twice was counted twice, and the step finished one approval early.
func TestAnIterationAlreadyCountedIsNotCountedAgain(t *testing.T) {
	h := newEngineHarness(t, "Counted Once Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h, approvalDefinition(h.projID, "purchase-approval-once", "parallel", ""),
		"ana", "budi", "citra")
	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}
	if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
		t.Fatalf("complete the first approval: %v", err)
	}

	err := h.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		instance, err := h.engine.GetInstanceForUpdate(txCtx, instanceID)
		if err != nil {
			return err
		}
		def, err := h.engine.GetProcessDefinition(txCtx, instance.Definition.ID)
		if err != nil {
			return err
		}
		return h.engine.ProceedIteration(txCtx, &instance, def, "approve", "0")
	})
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("reporting iteration 0 a second time: got %v, want a refusal", err)
	}

	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if completed, total, _ := instance.MultiInstanceProgress("approve"); completed != 1 || total != 3 {
		t.Fatalf("the step counts %d of %d, want 1 of 3", completed, total)
	}
}

// BPMN 2.0.2 §13.2.7 does not say what a multi-instance activity over an empty
// collection does; this engine runs the step once (see
// TestMultiInstance_WithAnEmptyCollectionRunsOnceAndProceeds). That one run has
// no instances to count, so a completion condition written against the
// counters has nothing to read and must not hold the process.
func TestAnApprovalNobodyWasListedForRunsOnceAndMovesOn(t *testing.T) {
	h := newEngineHarness(t, "Empty Approval Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h,
		approvalDefinition(h.projID, "purchase-approval-nobody", "parallel", "nrOfCompletedInstances >= 2"))

	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 1 {
		t.Fatalf("an empty list opened %d task(s), want the one run", len(open))
	}
	if open[0].IterationID != "" {
		t.Fatalf("the one run records iteration %q, want none", open[0].IterationID)
	}
	if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §13.2.7: the activity completes when all of its instances have,
// and a token then leaves it — also when the approval is a step inside a
// sub-process that is itself run once per item, one item at a time.
//
// The approval's tokens stayed on it after everybody had answered, the
// sub-process's end event never found the run empty, and the second item was
// never reached.
func TestARepeatingApprovalInsideASubProcessRunOneItemAtATimeFinishes(t *testing.T) {
	h := newEngineHarness(t, "Approval Inside Sequential Sub Project")
	ctx := h.Ctx()
	def := eachItem(h.projID, "approve-each-item", "sequential",
		[]*entities.Node{{ID: "approve", Type: entities.UserTask, Name: "Approve the item",
			MultiInstanceType: "parallel", Collection: "approvers"}},
		oneStepInside("approve"))
	h.deploy(t, def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, def.Key, map[string]any{
		"items": []any{"a", "b"}, "approvers": []any{"ana", "budi"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	for _, item := range []string{"a", "b"} {
		open := openIterationTasks(ctx, t, h, instanceID, "approve")
		if len(open) != 2 {
			t.Fatalf("item %s: two approvers were asked and %d task(s) are open", item, len(open))
		}
		for _, task := range open {
			if err := completeAs(ctx, h, task, "carol", nil); err != nil {
				t.Fatalf("item %s: approve: %v", item, err)
			}
		}
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != 0 {
		t.Fatalf("the finished approval still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, instanceID)
}
