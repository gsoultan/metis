package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/task"
)

// heldStatuses are the open states a task with an assignee can be in: taken,
// with a delegate, or raised.
var heldStatuses = []string{
	string(models.TaskClaimed),
	string(models.TaskDelegated),
	string(models.TaskEscalated),
}

func (r *taskRepository) ListOpenForPersonPaged(ctx context.Context, projectID uuid.UUID, username string, groups []string, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	n := p.Normalize()
	empty := contracts.NewPage([]models.TaskModel{}, 0, p)
	if projectID == uuid.Nil || username == "" {
		return empty, nil
	}
	// The project is asked of the tenant scope as well as named below: a
	// person's context carries their project's organization, and a project
	// outside it is not one they can see.
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible || len(scoped) == 0 {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}

	// Escaped (likeEscaper): an underscore in a name must not stand for any
	// character — "a_a" offered a task is not "aXa" offered it.
	unclaimed := task.Status.Eq(string(models.TaskUnclaimed))
	open := []task.Grp{
		task.And(task.Assignee.Eq(username), task.Status.In(heldStatuses...)),
		task.And(unclaimed, task.Assignee.IsNullOrEmpty(),
			task.CandidateUsers.TextLikeAny("%"+likeEscaper.Replace(quoted(username))+"%")),
	}
	if len(groups) > 0 {
		patterns := make([]string, len(groups))
		for i, group := range groups {
			patterns[i] = "%" + likeEscaper.Replace(quoted(group)) + "%"
		}
		open = append(open, task.And(unclaimed, task.Assignee.IsNullOrEmpty(),
			task.CandidateGroups.TextLikeAny(patterns...)))
	}
	q := task.New().Where(task.ProjectID.Eq([16]byte(projectID))).AnyOf(open...)

	total, err := q.Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count a person's tasks: %w", err)
	}
	rows, err := q.
		Order(task.CreatedAt.Desc(), task.ID.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(n.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list a person's tasks: %w", err)
	}
	items, err := tasksFrom(rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}
