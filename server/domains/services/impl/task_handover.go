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
)

// AssignTask gives a task to somebody, who holds it from then on.
func (s *taskService) AssignTask(ctx context.Context, id uuid.UUID, change servicecontracts.HandOver) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		step, err := s.openHandOver(txCtx, id, change, assigning)
		if err != nil {
			return err
		}
		task := step.task
		task.Assignee = &entities.User{Username: step.target}
		task.Status = entities.TaskClaimed
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskClaimed,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: map[string]any{"assignee": step.target},
		}, task, EventTaskAssigned, step.target)
		return nil
	})
}

// DelegateTask hands a task to somebody to work on.
func (s *taskService) DelegateTask(ctx context.Context, id uuid.UUID, change servicecontracts.HandOver) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		step, err := s.openHandOver(txCtx, id, change, delegating)
		if err != nil {
			return err
		}
		task := step.task
		task.Status = entities.TaskDelegated
		task.Assignee = &entities.User{Username: step.target}
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		}, task, EventTaskDelegated, step.target)
		return nil
	})
}

// UnclaimTask puts a claimed task back for its candidates to claim.
func (s *taskService) UnclaimTask(ctx context.Context, id uuid.UUID, change servicecontracts.HandOver) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Held, like every other write to a task. A release that read the
		// task while it was being completed waited for the completion's
		// commit and then wrote its own copy back — "unclaimed" over
		// "completed" — and the finished task was open again.
		row, err := s.lockedTask(txCtx, id)
		if err != nil {
			return err
		}
		task := adapters.TaskEntityAdapter{Model: row}.ToEntity()
		caller := handOverCallerFor(txCtx, change.Actor, task)
		if err := caller.mayRelease(); err != nil {
			return err
		}
		if err := refuseUnreleasable(task); err != nil {
			return err
		}
		if _, err := caller.reasonFor(change.Reason, "releasing this task"); err != nil {
			return err
		}
		task.Status = entities.TaskUnclaimed
		task.Assignee = nil
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		}, task, EventTaskUnclaimed, "")
		return nil
	})
}

// refuseUnreleasable refuses to release anything but a claimed task. It used
// to be an untyped error, which the transport answered as a 500 for what is
// the caller's mistake.
func refuseUnreleasable(task entities.Task) error {
	if err := refuseClosed(task, "released"); err != nil {
		return err
	}
	switch task.Status {
	case entities.TaskClaimed:
		return nil
	case entities.TaskUnclaimed:
		return apierr.Invalidf("nobody holds this task, so there is nothing to release")
	}
	return apierr.Invalidf("this task cannot be released while it is %s", task.Status)
}
