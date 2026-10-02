package impl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// UpdateTask changes the fields of a task that the edit carries, and records
// who changed which, from what, to what.
//
// It used to write name, priority and due date together from whatever the
// request held, so an edit carrying one of them blanked the other two. An edit
// that changes nothing writes nothing and records nothing.
func (s *taskService) UpdateTask(ctx context.Context, id uuid.UUID, edit servicecontracts.TaskEdit) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Held for the reason a release holds it: the whole row is written
		// back, status included, so an unheld read could reopen a task
		// completed in between.
		row, err := s.lockedTask(txCtx, id)
		if err != nil {
			return err
		}
		task := adapters.TaskEntityAdapter{Model: row}.ToEntity()
		caller := handOverCallerFor(txCtx, edit.Actor, task)
		if err := caller.mayEdit(); err != nil {
			return err
		}
		if err := refuseClosed(task, "changed"); err != nil {
			return err
		}
		reason, err := caller.reasonFor(edit.Reason, "changing this task")
		if err != nil {
			return err
		}
		holder := task.AssigneeUsername()
		changes, err := applyEdit(&task, edit)
		if err != nil || len(changes) == 0 {
			return err
		}
		// The whole row is written back. An owner or a pending mark that no
		// longer describes the task is not carried on by an edit.
		if task.HasStaleDelegation() {
			task.Owner, task.DelegationState = nil, ""
		}
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
		}, task, EventTaskEdited, handOverRecord{
			actor:   caller.username,
			holder:  holder,
			reason:  reason,
			changes: changes,
		})
	})
}

// applyEdit changes the fields of task that edit carries and returns each one
// that actually changed, with what it was and what it is.
func applyEdit(task *entities.Task, edit servicecontracts.TaskEdit) (map[string]any, error) {
	changes := map[string]any{}
	if edit.Name != nil {
		name := strings.TrimSpace(*edit.Name)
		if name == "" {
			return nil, apierr.Invalidf("a task needs a name; leave name out of the request to keep the one it has")
		}
		if name != task.Name {
			changes["name"] = fieldChange(task.Name, name)
			task.Name = name
		}
	}
	if edit.Priority != nil && *edit.Priority != task.Priority {
		changes["priority"] = fieldChange(task.Priority, *edit.Priority)
		task.Priority = *edit.Priority
	}
	switch {
	case edit.ClearDueDate:
		if task.DueDate != nil {
			changes["due_date"] = fieldChange(dueText(task.DueDate), nil)
			task.DueDate = nil
		}
	case edit.DueDate != nil && !sameSecond(task.DueDate, *edit.DueDate):
		due := *edit.DueDate
		changes["due_date"] = fieldChange(dueText(task.DueDate), dueText(&due))
		task.DueDate = &due
	}
	return changes, nil
}

func fieldChange(before, after any) map[string]any {
	return map[string]any{"before": before, "after": after}
}

// dueText is a due date as the trail keeps it: RFC 3339 in UTC, or nil for none.
func dueText(at *time.Time) any {
	if at == nil {
		return nil
	}
	return at.UTC().Format(time.RFC3339)
}

// sameSecond reports whether a task's due date is the one it is being given,
// to the second.
//
// Clients hold a due date as RFC 3339 text in whole seconds and send it back
// with every save, while the stored one — "two hours after the task appeared"
// — has a fraction. Compared exactly, every save of an untouched task changed
// its due date.
func sameSecond(current *time.Time, next time.Time) bool {
	return current != nil && current.Truncate(time.Second).Equal(next.Truncate(time.Second))
}
