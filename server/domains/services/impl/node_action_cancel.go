package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// cancel ends an instance where it stands, and returns the row as it wrote it
// and the tasks it withdrew as they were before.
//
// It is cancelWhole, for a caller that records the instance and its tasks and
// nothing else of what a cancel takes: the migration.
func (a nodeActions) cancel(
	ctx context.Context,
	locked models.ProcessInstanceModel,
) (ended models.ProcessInstanceModel, withdrawn []models.TaskModel, err error) {
	done, err := a.cancelWhole(ctx, locked)
	if err != nil {
		return models.ProcessInstanceModel{}, nil, err
	}
	return done.instance, done.withdrawn, nil
}

// cancelWhole ends an instance where it stands, and returns what that did.
//
// locked is the row its caller locked: it is the one written back, so nothing
// that landed before the lock is undone.
//
// The tasks are withdrawn, announced and recorded from their rows as they are
// once held (heldOpen), not as a read before that found them: a claim does not
// take the instance, so it is not kept out by the caller's lock, and a task
// claimed as the instance was cancelled was withdrawn from somebody the record
// called nobody and nobody told.
//
// Work parked for outside workers is withdrawn too (withdrawParked). It is a
// row of its own, like a task, and it used to outlive the instance: a worker
// was still handed it, and its report moved the cancelled instance on.
//
// Tokens are cleared and the status is set rather than the rows deleted: what
// this instance did, and how far it got, is the record somebody will ask for.
// Pending timers and queued calls are left alone deliberately — JobRepository
// has no delete. timerStillApplies refuses to fire a timer for an instance
// that is not active, and a queued call asks whether its instance has ended
// before it is made (jobService.executeServiceTask).
func (a nodeActions) cancelWhole(ctx context.Context, locked models.ProcessInstanceModel) (cancellation, error) {
	instanceID := uuid.UUID(locked.ID)
	open, err := a.heldOpen(ctx, instanceID)
	if err != nil {
		return cancellation{}, err
	}
	var withdrawn []models.TaskModel
	for _, task := range open {
		if err := a.repo.Task().UpdateStatus(ctx, uuid.UUID(task.ID), models.TaskCanceled); err != nil {
			return cancellation{}, err
		}
		a.announceWithdrawal(ctx, task)
		withdrawn = append(withdrawn, task)
	}
	parked, err := a.withdrawParked(ctx, locked)
	if err != nil {
		return cancellation{}, err
	}
	// Waiting events go with it. An instance that will never run again
	// cannot honour a subscription, and leaving one means a message arrives
	// later and correlates to something that has stopped.
	subs, err := a.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		return cancellation{}, err
	}
	for _, sub := range subs {
		if err := a.repo.Subscription().Delete(ctx, uuid.UUID(sub.ID)); err != nil {
			return cancellation{}, err
		}
	}
	locked.Tokens = nil
	locked.Status = models.ProcessCancelled
	if err := a.repo.Process().Update(ctx, locked); err != nil {
		return cancellation{}, err
	}
	return cancellation{instance: locked, withdrawn: withdrawn, parkedWithdrawn: parked}, nil
}

// withdrawParked takes the work an instance has parked for outside workers
// off the list they fetch from, and returns how many pieces it took.
//
// It is the engine's own withdrawal (Engine.withdrawExternalTasksOn), the one
// an ad-hoc sub-process uses on the steps still running inside it when it
// finishes: the rows are deleted, and each step that had work parked gets a
// line on the instance's trail saying it was withdrawn, since a row that is
// gone reads otherwise as work somebody did. Called with the instance held,
// which is the order everything that takes both takes them in.
//
// An instance with nothing parked costs the one read that finds that out. One
// with work parked, in a wiring whose engine cannot withdraw it, is refused:
// leaving the work on the list is the defect this closes, and nothing on the
// execution path is silent.
func (a nodeActions) withdrawParked(ctx context.Context, locked models.ProcessInstanceModel) (int, error) {
	instanceID := uuid.UUID(locked.ID)
	parked, err := a.repo.ExternalTask().ListByProcessInstance(ctx, instanceID)
	if err != nil {
		return 0, fmt.Errorf("reading the work instance %s has parked for workers: %w", instanceID, err)
	}
	if len(parked) == 0 {
		return 0, nil
	}
	if a.parked == nil {
		return 0, fmt.Errorf("instance %s has work parked for workers, and this server was wired without the engine that withdraws it", instanceID)
	}
	def, err := a.engine.GetProcessDefinition(ctx, uuid.UUID(locked.DefinitionID))
	if err != nil {
		return 0, fmt.Errorf("reading the process instance %s runs, to withdraw its parked work: %w", instanceID, err)
	}
	steps := make([]*entities.Node, 0, len(parked))
	seen := make(map[string]bool, len(parked))
	for _, task := range parked {
		if seen[task.NodeID] {
			continue
		}
		seen[task.NodeID] = true
		step := def.FindNode(task.NodeID)
		if step == nil {
			// Work parked on a step the graph no longer has is still withdrawn.
			step = &entities.Node{ID: task.NodeID}
		}
		steps = append(steps, step)
	}
	instance := adapters.InstanceEntityAdapter{Model: locked}.ToEntity()
	if err := a.parked.withdrawExternalTasksOn(ctx, &instance, steps); err != nil {
		return 0, err
	}
	return len(parked), nil
}
