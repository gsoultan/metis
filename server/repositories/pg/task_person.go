package pg

import (
	"context"
	"fmt"
	"strings"

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

	args := []any{uuidsToRaw([]uuid.UUID{projectID}), username, heldStatuses, string(models.TaskUnclaimed)}
	// Escaped (likeEscaper): an underscore in a name must not stand for any
	// character — "a_a" offered a task is not "aXa" offered it.
	offered := []string{fmt.Sprintf(`candidate_users LIKE $%d`, len(args)+1)}
	args = append(args, "%"+likeEscaper.Replace(quoted(username))+"%")
	for _, group := range groups {
		offered = append(offered, fmt.Sprintf(`candidate_groups LIKE $%d`, len(args)+1))
		args = append(args, "%"+likeEscaper.Replace(quoted(group))+"%")
	}
	predicate := "deleted_at IS NULL AND project_id = ANY($1) AND (" +
		"(assignee = $2 AND status = ANY($3)) OR " +
		"(status = $4 AND COALESCE(assignee, '') = '' AND (" + strings.Join(offered, " OR ") + ")))"

	var total int64
	countRows, err := ex.Query(ctx, "SELECT COUNT(*) FROM tasks WHERE "+predicate, args)
	if err != nil {
		return empty, fmt.Errorf("could not count a person's tasks: %w", err)
	}
	if countRows.Next() {
		if err := scanInt(countRows.RawValues(), &total); err != nil {
			countRows.Close()
			return empty, err
		}
	}
	countRows.Close()
	if err := countRows.Err(); err != nil {
		return empty, fmt.Errorf("could not count a person's tasks: %w", err)
	}

	query := "SELECT id FROM tasks WHERE " + predicate + " ORDER BY " + newestFirst +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, n.PageSize, n.Offset())
	idRows, err := ex.Query(ctx, query, args)
	if err != nil {
		return empty, fmt.Errorf("could not list a person's tasks: %w", err)
	}
	var ids [][16]byte
	for idRows.Next() {
		values := idRows.RawValues()
		if len(values) == 0 {
			continue
		}
		var id [16]byte
		copy(id[:], values[0])
		ids = append(ids, id)
	}
	idRows.Close()
	if err := idRows.Err(); err != nil {
		return empty, fmt.Errorf("could not list a person's tasks: %w", err)
	}
	if len(ids) == 0 {
		return contracts.NewPage([]models.TaskModel{}, total, p), nil
	}
	rows, err := task.New().
		Where(task.ID.In(ids...)).
		Order(task.CreatedAt.Desc(), task.ID.Desc()).
		Limit(int64(len(ids))).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not read a person's tasks: %w", err)
	}
	items, err := tasksFrom(rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}
