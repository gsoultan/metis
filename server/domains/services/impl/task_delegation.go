package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// ResolveTask hands a delegated task back to its owner.
//
// The delegate does it when their part is done; an administrator may do it for
// them, saying why. The task is the owner's claim again afterwards, and they
// complete it as they would have.
func (s *taskService) ResolveTask(ctx context.Context, id uuid.UUID, change servicecontracts.HandOver) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		row, err := s.lockedTask(txCtx, id)
		if err != nil {
			return err
		}
		task := adapters.TaskEntityAdapter{Model: row}.ToEntity()
		caller := handOverCallerFor(txCtx, change.Actor, task)
		if err := caller.mayResolve(); err != nil {
			return err
		}
		if err := refuseClosed(task, "handed back"); err != nil {
			return err
		}
		if !task.AwaitsHandBack() {
			return apierr.Invalidf("this task has not been delegated, so there is nothing to hand back")
		}
		reason, err := caller.reasonFor(change.Reason, "handing this task back")
		if err != nil {
			return err
		}
		delegate, owner := task.AssigneeUsername(), task.OwnerUsername()
		task.Assignee = &entities.User{Username: owner}
		task.Status = entities.TaskClaimed
		task.DelegationState = entities.DelegationResolved
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		return s.announceHandOver(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		}, task, EventTaskResolved, handOverRecord{
			actor:          caller.username,
			previousHolder: delegate,
			target:         owner,
			owner:          owner,
			reason:         reason,
		})
	})
}

// completionWaitsForHandBack refuses to complete a task that is with a
// delegate. The delegate did the work and does not give the approval; the
// owner gives it and has to have the task back first.
func completionWaitsForHandBack(task entities.Task, userID string) error {
	owner, delegate := task.OwnerUsername(), task.AssigneeUsername()
	if userID == owner {
		return apierr.Forbiddenf("this task is with %s, who has to hand it back to you before you can complete it", delegate)
	}
	return apierr.Forbiddenf("this task was delegated by %s, who completes it; hand it back to %s first", owner, owner)
}

// ListTasksDelegatedByPaged returns one page of the tasks somebody delegated
// that are still with their delegate.
func (s *taskService) ListTasksDelegatedByPaged(ctx context.Context, owner string, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error) {
	result, err := s.repo.Task().ListDelegatedByPaged(ctx, owner, page)
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}
	tasks := make([]entities.Task, len(result.Items))
	for i, m := range result.Items {
		tasks[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(tasks, result.Total, page), nil
}
