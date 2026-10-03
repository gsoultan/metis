package impl

import (
	"context"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/repositories/models"
)

// cancel ends an instance where it stands, and returns the row as it wrote it
// and the tasks it withdrew as they were before.
//
// locked is the row its caller locked: it is the one written back, so nothing
// that landed before the lock is undone.
//
// Tokens are cleared and the status is set rather than the rows deleted: what
// this instance did, and how far it got, is the record somebody will ask for.
// Pending timers are left alone deliberately — JobRepository has no delete, and
// timerStillApplies already refuses to fire one for an instance that is not
// active, which is the same thing a terminate end event relies on.
func (a nodeActions) cancel(
	ctx context.Context,
	locked models.ProcessInstanceModel,
) (ended models.ProcessInstanceModel, withdrawn []models.TaskModel, err error) {
	instanceID := uuid.UUID(locked.ID)
	tasks, err := a.repo.Task().ListByInstance(ctx, instanceID)
	if err != nil {
		return models.ProcessInstanceModel{}, nil, err
	}
	for _, task := range tasks {
		if !openTask(task.Status) {
			continue
		}
		if err := a.repo.Task().UpdateStatus(ctx, uuid.UUID(task.ID), models.TaskCanceled); err != nil {
			return models.ProcessInstanceModel{}, nil, err
		}
		a.announceWithdrawal(ctx, task)
		withdrawn = append(withdrawn, task)
	}
	// Waiting events go with it. An instance that will never run again
	// cannot honour a subscription, and leaving one means a message arrives
	// later and correlates to something that has stopped.
	subs, err := a.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		return models.ProcessInstanceModel{}, nil, err
	}
	for _, sub := range subs {
		if err := a.repo.Subscription().Delete(ctx, uuid.UUID(sub.ID)); err != nil {
			return models.ProcessInstanceModel{}, nil, err
		}
	}
	locked.Tokens = nil
	locked.Status = models.ProcessCancelled
	if err := a.repo.Process().Update(ctx, locked); err != nil {
		return models.ProcessInstanceModel{}, nil, err
	}
	return locked, withdrawn, nil
}
