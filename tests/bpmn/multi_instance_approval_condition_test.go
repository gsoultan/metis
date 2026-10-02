package bpmn_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	observercontracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// withdrawals records the task-withdrawn events the engine raises — what the
// notification observer turns into somebody being told, and the audit
// observer into a line on the trail.
type withdrawals struct {
	events []entities.ProcessEvent
}

func (w *withdrawals) OnEvent(_ context.Context, event entities.ProcessEvent) {
	if event.Type == entities.EventTaskCanceled {
		w.events = append(w.events, event)
	}
}

var _ observercontracts.ProcessObserver = (*withdrawals)(nil)

// requireRefusedInPlainWords fails unless err is a refusal a person can read:
// the caller's-mistake kind, and naming neither the task's id nor its step's.
func requireRefusedInPlainWords(t *testing.T, err error, task entities.Task) {
	t.Helper()
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("got %v, want a refusal the caller can act on", err)
	}
	if text := err.Error(); strings.Contains(text, task.ID.String()) || strings.Contains(text, task.NodeID()) {
		t.Fatalf("the refusal names an id a person cannot read: %q", text)
	}
}

// BPMN 2.0.2 §10.3.8 (MultiInstanceLoopCharacteristics.completionCondition): an
// expression that, when it evaluates to true, cancels the remaining activity
// instances and produces a token. §13.2.7 gives the same semantics.
//
// The engine dropped its count and moved on. The third approver's task stayed
// in their inbox for a step the process had left, and its token on the step.
func TestTwoOfThreeApprovalsEndTheStepAndWithdrawTheThird(t *testing.T) {
	h := newEngineHarness(t, "Two Of Three Project")
	ctx := h.Ctx()
	watcher := &withdrawals{}
	h.dispatcher.Register(watcher)
	h.dispatcher.Register(observersimpl.NewAuditLogObserver(h.repo.Audit()))

	instanceID := startApproval(t, h,
		approvalDefinition(h.projID, "two-of-three", "parallel", "nrOfCompletedInstances >= 2"),
		"ana", "budi", "citra")
	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}
	// The third approver has picked theirs up, so there is somebody to tell.
	if err := h.svc.ClaimTask(testutils.AsOperator(ctx, "citra"), open[2].ID, "citra"); err != nil {
		t.Fatalf("claim the third approval: %v", err)
	}

	if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
		t.Fatalf("first approval: %v", err)
	}
	if h.waitingAt(ctx, t, instanceID, "record") {
		t.Fatal("one approval of the two required moved the process on")
	}
	if err := completeAs(ctx, h, open[1], "carol", nil); err != nil {
		t.Fatalf("second approval: %v", err)
	}

	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("after the second approval the process moved on %d times, want once", seen)
	}
	third, err := h.svc.GetTask(ctx, open[2].ID)
	if err != nil {
		t.Fatalf("re-read the third approval: %v", err)
	}
	if third.Status != entities.TaskCanceled {
		t.Fatalf("the approval nobody needs any more is %q, want it withdrawn", third.Status)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != 0 {
		t.Fatalf("the finished step still holds tokens %v", left)
	}

	// Told the way a deadline or a migration tells somebody their task went.
	if len(watcher.events) != 1 {
		t.Fatalf("withdrawing one task raised %d withdrawal events", len(watcher.events))
	}
	if event := watcher.events[0]; event.Assignee != "citra" || event.Node == nil || event.Node.ID != "approve" || event.Instance == nil {
		t.Fatalf("the withdrawal does not say whose task, on which step, of which instance: %+v", event)
	}
	trail, err := h.engine.GetAuditLogs(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	told := false
	for _, entry := range trail {
		if entry.Type == entities.EventTaskCanceled && strings.Contains(entry.Narrative, "no longer needed and has been withdrawn") {
			told = true
		}
	}
	if !told {
		t.Error("the trail does not say the third approval was withdrawn")
	}

	// And it stays withdrawn.
	requireRefusedInPlainWords(t, completeAs(ctx, h, third, "citra", nil), third)
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("completing a withdrawn approval moved the process on again: %d times", seen)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §13.2.7: the activity completes when its instances have, or when
// the completionCondition holds — whichever comes first.
//
// The condition replaced "everyone has answered" instead of adding to it. Two
// of N, with one approver on the list, never read true, and the step waited
// for a second approval nobody had been asked for.
func TestAThresholdLargerThanTheListFinishesWhenEveryoneHasAnswered(t *testing.T) {
	h := newEngineHarness(t, "Short List Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h,
		approvalDefinition(h.projID, "two-of-one", "parallel", "nrOfCompletedInstances >= 2"), "ana")

	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 1 {
		t.Fatalf("one approver was asked and %d task(s) are open", len(open))
	}
	if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
		t.Fatalf("the only approval: %v", err)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §10.3.8: once the completionCondition holds the remaining
// instances are cancelled, so none of them can complete afterwards.
//
// "Any rejection ends the approval", written on what the approver decided. The
// first rejection moved the process on and left the other approvals open; a
// second approver rejecting found no count, read the condition true again, and
// moved the process on a second time.
func TestASecondRejectionCannotMoveTheProcessOnAgain(t *testing.T) {
	h := newEngineHarness(t, "Rejection Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h,
		approvalDefinition(h.projID, "any-rejection-ends-it", "parallel", `decision = "reject"`),
		"ana", "budi", "citra")
	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}

	if err := completeAs(ctx, h, open[0], "carol", map[string]any{"decision": "reject"}); err != nil {
		t.Fatalf("the first rejection: %v", err)
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("a rejection moved the process on %d times, want once", seen)
	}

	second, err := h.svc.GetTask(ctx, open[1].ID)
	if err != nil {
		t.Fatalf("re-read the second approval: %v", err)
	}
	requireRefusedInPlainWords(t, completeAs(ctx, h, second, "carol", map[string]any{"decision": "reject"}), second)
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("a second rejection moved the process on again: %d times", seen)
	}
	if stillOpen := h.openTasksOn(t, instanceID, "approve"); stillOpen != 0 {
		t.Fatalf("%d approval(s) are still in somebody's inbox for a step that ended", stillOpen)
	}
}

// BPMN 2.0.2 §13.2.3 (Task): a task that has completed or been withdrawn is no
// longer an instance of anything, and cannot complete.
//
// A withdrawn task was refused with its id in the text, and a status the
// service did not recognise was not refused at all: absent constraint means
// deny, and "not completed and not cancelled" is not the same as open.
func TestCompletingATaskThatIsNotOpenIsRefusedBeforeTheEngine(t *testing.T) {
	h := newEngineHarness(t, "Closed Task Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "one-approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the purchase"},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	})

	for _, tc := range []struct {
		status models.TaskStatus
		says   string
	}{
		{models.TaskCompleted, "already completed"},
		{models.TaskCanceled, "withdrawn"},
		{models.TaskStatus("archived"), "not open"},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			instanceID, err := h.svc.StartProcess(ctx, h.projID, "one-approval", nil)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			open := openIterationTasks(ctx, t, h, instanceID, "approve")
			if len(open) != 1 {
				t.Fatalf("the approval opened %d task(s), want 1", len(open))
			}
			task := open[0]
			closeTask(ctx, t, h, task.ID, tc.status)

			err = completeAs(ctx, h, task, "carol", nil)
			requireRefusedInPlainWords(t, err, task)
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal reads %q, want it to say the task is %s", err, tc.says)
			}
			// The engine was never asked: the token is where it was and
			// nothing followed the step.
			if left := tokenIterationsOn(ctx, t, h, instanceID, "approve"); len(left) != 1 {
				t.Fatalf("the refused completion moved the token: %v", left)
			}
			if seen := tasksEverOn(t, h, instanceID, "record"); seen != 0 {
				t.Fatalf("the refused completion moved the process on %d time(s)", seen)
			}
		})
	}
}

// closeTask sets a task's status the way something other than a completion
// would have: a withdrawal, an import, a hand-applied fix.
func closeTask(ctx context.Context, t *testing.T, h engineHarness, taskID uuid.UUID, status models.TaskStatus) {
	t.Helper()
	if err := h.repo.Task().UpdateStatus(ctx, taskID, status); err != nil {
		t.Fatalf("set the task %s: %v", status, err)
	}
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
