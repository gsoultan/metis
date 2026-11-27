package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/server/repositories/store/workflowuser"
	"github.com/gsoultan/storm/runtime"
)

// openTaskStatuses are the states of a task that is still somebody's work.
var openTaskStatuses = append([]string{string(models.TaskUnclaimed)}, heldStatuses...)

// openTasksNaming selects a project's open tasks that name username: as their
// assignee, or among the people they are offered to.
//
// Escaped and quoted the way the People inbox matches, so "a_a" does not
// match "aXa" and "ada" does not match "adam".
func openTasksNaming(projectID uuid.UUID, username string) task.Query {
	return task.New().
		Where(task.ProjectID.Eq([16]byte(projectID)), task.Status.In(openTaskStatuses...)).
		AnyOf(
			task.And(task.Assignee.Eq(username)),
			task.And(task.CandidateUsers.TextLikeAny("%"+likeEscaper.Replace(quoted(username))+"%")),
		)
}

// CountOpenTasksNaming reports how many of a project's open tasks name a
// username, which is what a rename would leave behind.
func (r *workflowUserRepository) CountOpenTasksNaming(ctx context.Context, projectID uuid.UUID, username string) (int, error) {
	if err := r.scope.requireProjectInTenant(ctx, projectID); err != nil {
		return 0, err
	}
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	total, err := openTasksNaming(projectID, username).Count(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not count the open tasks naming %q: %w", username, err)
	}
	return int(total), nil
}

// Rename gives a project's participant a new username, and with
// moveOpenTasks, carries the open tasks naming the old one over to it.
//
// One transaction: a rename that landed without its tasks would leave work
// addressed to a name nobody has, and tasks moved to a name nobody has yet
// would be the same thing the other way round. Completed tasks keep the name
// they were done under, because that is what happened.
func (r *workflowUserRepository) Rename(ctx context.Context, projectID, id uuid.UUID, to string, moveOpenTasks bool) (int, error) {
	if err := r.scope.requireProjectInTenant(ctx, projectID); err != nil {
		return 0, err
	}
	moved := 0
	err := r.conn.Transact(ctx, func(ctx context.Context) error {
		ex, err := r.conn.Executor(ctx)
		if err != nil {
			return err
		}
		row, found, err := workflowuser.New().
			Where(workflowuser.ID.Eq(id), workflowuser.ProjectID.Eq(projectID)).
			One(ctx, ex)
		if err != nil {
			return fmt.Errorf("could not read the participant: %w", err)
		}
		if !found {
			return fmt.Errorf("%w: no such participant in this project", apierr.ErrNotFound)
		}
		from := row.Username
		if from == to {
			return nil
		}
		if moveOpenTasks {
			if moved, err = moveTasks(ctx, ex, projectID, from, to); err != nil {
				return err
			}
		}
		mut := workflowuser.Mutate(row)
		mut.SetUsername(to)
		if err := mut.Update(ctx, ex); err != nil {
			return fmt.Errorf("could not rename %q to %q: %w", from, to, err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return moved, nil
}

// moveTasks readdresses the open tasks naming from to name to instead.
func moveTasks(ctx context.Context, ex runtime.Executor, projectID uuid.UUID, from, to string) (int, error) {
	rows, err := openTasksNaming(projectID, from).All(ctx, ex, nil)
	if err != nil {
		return 0, fmt.Errorf("could not read the open tasks naming %q: %w", from, err)
	}
	moved := 0
	for _, row := range rows {
		mut := task.Mutate(row)
		changed := false
		if assignee, ok := row.Assignee.Get(); ok && assignee == from {
			mut.SetAssignee(to)
			changed = true
		}
		users, replaced, err := replaceCandidate(row.CandidateUsers, from, to)
		if err != nil {
			return moved, fmt.Errorf("could not read task %s's candidates: %w", uuid.UUID(row.ID), err)
		}
		if replaced {
			mut.SetCandidateUsers(users)
			changed = true
		}
		if !changed {
			// Matched by the pattern but not by name, which the quoting is
			// meant to prevent; nothing to move.
			continue
		}
		if err := mut.Update(ctx, ex); err != nil {
			return moved, fmt.Errorf("could not move task %s to %q: %w", uuid.UUID(row.ID), to, err)
		}
		moved++
	}
	return moved, nil
}

// replaceCandidate swaps from for to in a task's candidate users, keeping the
// list free of a duplicate when to was already there.
func replaceCandidate(raw runtime.JSON, from, to string) (runtime.JSON, bool, error) {
	if len(raw) == 0 {
		return raw, false, nil
	}
	var users []string
	if err := json.Unmarshal(raw, &users); err != nil {
		return raw, false, err
	}
	at := slices.Index(users, from)
	if at < 0 {
		return raw, false, nil
	}
	if slices.Contains(users, to) {
		users = slices.Delete(users, at, at+1)
	} else {
		users[at] = to
	}
	encoded, err := json.Marshal(users)
	if err != nil {
		return raw, false, err
	}
	return runtime.JSON(encoded), true, nil
}
