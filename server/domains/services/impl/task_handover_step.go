package impl

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// handOverStep is a hand-over that has been admitted: the task as it stands
// under its row lock, who asked, and what they said.
type handOverStep struct {
	row    models.TaskModel
	task   entities.Task
	caller handOverCaller
	reason string
	target string
	// candidateOverride: an administrator sent the task to somebody it was not
	// offered to, with a reason. The trail says so.
	candidateOverride bool
}

// openHandOver reads the task a hand-over is about, holding its row, and
// decides whether it may be made.
//
// In this order, because the order is what a refusal reveals: whether the
// caller may hand the task on at all comes first, so somebody with no business
// here learns nothing else about it. Then whether the task can still be handed
// on, who it goes to, and why. Last, whether the person it goes to may have it
// (admitTarget) — the only part that reads anything but the task.
//
// Holding the row matters as much as the checks. Completion writes it too, so
// a hand-over that read the task open while it was being completed would write
// it back open; and a second hand-over made from the same read would be judged
// against a holder the task no longer has.
func (s *taskService) openHandOver(ctx context.Context, id uuid.UUID, change servicecontracts.HandOver, kind handOverKind) (handOverStep, error) {
	row, err := s.lockedTask(ctx, id)
	if err != nil {
		return handOverStep{}, err
	}
	task := adapters.TaskEntityAdapter{Model: row}.ToEntity()
	caller := handOverCallerFor(ctx, change.Actor, task)
	if err := caller.mayHandOver(task); err != nil {
		return handOverStep{}, err
	}
	if err := refuseClosed(task, kind.done); err != nil {
		return handOverStep{}, err
	}
	if task.AwaitsHandBack() {
		// One way back, so the trail of who had it stays a line: the delegate
		// hands it back, and its owner hands it on.
		return handOverStep{}, apierr.Invalidf("this task was delegated to %s by %s; it has to be handed back to %s before it can be %s",
			task.AssigneeUsername(), task.OwnerUsername(), task.OwnerUsername(), kind.done)
	}
	if kind.needsHolder && task.AssigneeUsername() == "" {
		return handOverStep{}, apierr.Invalidf("nobody holds this task, so there is nobody for it to come back to; claim or assign it first")
	}
	target := strings.TrimSpace(change.Target)
	if target == "" {
		return handOverStep{}, apierr.Invalidf("%s", kind.missingTarget)
	}
	reason, err := caller.reasonFor(change.Reason, kind.doing)
	if err != nil {
		return handOverStep{}, err
	}
	step := handOverStep{row: row, task: task, caller: caller, reason: reason, target: target}
	if step.candidateOverride, err = s.admitTarget(ctx, step); err != nil {
		return handOverStep{}, err
	}
	return step, nil
}

// refuseClosed refuses to change a task nobody can work on any more. A
// completed task handed on would be open again, and completing it again would
// run everything after it a second time.
func refuseClosed(task entities.Task, done string) error {
	switch task.Status {
	case entities.TaskCompleted:
		return apierr.Invalidf("this task is completed; it cannot be %s", done)
	case entities.TaskCanceled:
		return apierr.Invalidf("this task was withdrawn; it cannot be %s", done)
	}
	return nil
}
