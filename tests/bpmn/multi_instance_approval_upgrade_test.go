package bpmn_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// An approval that ended early under the release before migration 31 was left
// in a state this release never produces: the step stopped counting, kept a
// token for every approver, and left the approvals nobody had given open in
// their inboxes. The process had already moved on.
//
// These tests stage that state and say what this release does with it.

// endedEarlyBeforeTheUpgrade takes a two-of-three approval to the point where
// two have approved and the process has moved on, then puts back what the
// earlier release would have left behind: a token for every approver, and the
// third approval open with no iteration recorded. It returns the third task.
func endedEarlyBeforeTheUpgrade(t *testing.T, h engineHarness, def *entities.ProcessDefinition) (uuid.UUID, entities.Task) {
	t.Helper()
	ctx := h.Ctx()
	instanceID := startApproval(t, h, def, "ana", "budi", "citra")
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

	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	approve := &entities.Node{ID: "approve"}
	for _, run := range []string{"0", "1", "2"} {
		instance.AddTokenWithIteration(approve, run)
	}
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("put the earlier release's tokens back: %v", err)
	}
	if err := h.repo.Task().UpdateStatus(ctx, open[2].ID, models.TaskUnclaimed); err != nil {
		t.Fatalf("reopen the approval the earlier release left open: %v", err)
	}
	forgetIterations(t, h, instanceID)

	left := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(left) != 1 || left[0].IterationID != "" {
		t.Fatalf("staging failed: %d approval(s) open, want the one the earlier release left", len(left))
	}
	if _, _, counting := instance.MultiInstanceProgress("approve"); counting {
		t.Fatal("staging failed: the step is still counting")
	}
	return instanceID, left[0]
}

// BPMN 2.0.2 §10.3.8: once the completionCondition holds the remaining
// instances are cancelled, so none of them can complete afterwards.
//
// The approval the earlier release left open is refused when somebody
// completes it, in words they can read, and nothing moves: the step finished
// before the upgrade and the process is past it. The task stays where it is
// and the stale tokens stay on the step — clearing them is the operator's
// decision, which docs/upgrading.md gives the queries for.
func TestAnApprovalLeftOpenByAStepThatEndedBeforeTheUpgradeIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Ended Before Upgrade Project")
	ctx := h.Ctx()
	instanceID, leftover := endedEarlyBeforeTheUpgrade(t, h,
		approvalDefinition(h.projID, "ended-before-upgrade", "parallel", "nrOfCompletedInstances >= 2"))

	err := completeAs(ctx, h, leftover, "carol", nil)
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("completing the approval the earlier release left open got %v, want a refusal", err)
	}
	if text := err.Error(); !strings.Contains(text, "already finished") ||
		strings.Contains(text, "approve") || strings.Contains(text, leftover.ID.String()) {
		t.Fatalf("the refusal does not say the step has finished in words a person can read: %q", text)
	}

	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("the refused completion moved the process on again: %d times", seen)
	}
	if still := openIterationTasks(ctx, t, h, instanceID, "approve"); len(still) != 1 {
		t.Fatalf("the refused completion left %d approval(s) open, want the one it was refused for", len(still))
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); !slices.Equal(left, []string{"0", "1", "2"}) {
		t.Fatalf("the refused completion changed the step's tokens to %v", left)
	}

	// The process carries on from where it was, and — as before the upgrade —
	// cannot end while the stale tokens are there.
	completeTaskAt(ctx, t, h, instanceID, "record", nil)
	requireInstanceStatus(ctx, t, h, instanceID, entities.ProcessActive)
}

// BPMN 2.0.2 §13.4.3 (Intermediate Boundary Events): a boundary event is live
// only while its activity is; once the activity has completed, the event can
// no longer occur.
//
// A deadline still queued for an approval that ended before the upgrade found
// the tokens the earlier release left on the step, took them for approvers
// still being waited on, and fired: the process went down its escalation path
// from a step it had left, alongside the path it was already on.
func TestADeadlineOnAnApprovalThatEndedBeforeTheUpgradeDoesNotFire(t *testing.T) {
	h := newEngineHarness(t, "Deadline Ended Before Upgrade Project")
	ctx := h.Ctx()
	def := approvalDefinition(h.projID, "deadline-ended-before-upgrade", "parallel", "nrOfCompletedInstances >= 2")
	def.Nodes = append(def.Nodes,
		&entities.Node{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "approve",
			Properties: map[string]any{"timer_duration": "PT2H"}},
		&entities.Node{ID: "escalate", Type: entities.UserTask, Name: "Escalate to the manager"})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "d1", SourceRef: "deadline", TargetRef: "escalate"},
		&entities.SequenceFlow{ID: "d2", SourceRef: "escalate", TargetRef: "end"})
	instanceID, _ := endedEarlyBeforeTheUpgrade(t, h, def)

	// Two hours pass.
	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Fatal("no deadline was waiting to come due")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if seen := tasksEverOn(t, h, instanceID, "escalate"); seen != 0 {
		t.Fatalf("the deadline fired %d time(s) on an approval that had already ended", seen)
	}
	if !h.waitingAt(ctx, t, instanceID, "record") {
		t.Fatal("the deadline took the process off the step it was waiting on")
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("the process moved past the approval %d times, want once", seen)
	}
}
