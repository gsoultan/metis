package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/storm/runtime"
)

type taskRepository struct{ conn }

// NewTaskRepository returns the store of human work.
func NewTaskRepository(c *db.Conn) contracts.TaskRepository {
	return &taskRepository{conn{conn: c}}
}

func (r *taskRepository) Get(ctx context.Context, id uuid.UUID) (models.TaskModel, error) {
	return r.one(ctx, id, false)
}

// GetForUpdate reads a task and holds its row until the transaction ends, so a
// decision made from the read — claim it, hand it over — cannot be overtaken by
// another made from the same read.
func (r *taskRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (models.TaskModel, error) {
	return r.one(ctx, id, true)
}

func (r *taskRepository) one(ctx context.Context, id uuid.UUID, forUpdate bool) (models.TaskModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return models.TaskModel{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.TaskModel{}, err
	}
	q := task.New().Where(task.ID.Eq(id))
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return models.TaskModel{}, fmt.Errorf("%w: no such task", apierr.ErrNotFound)
		}
		q = q.Where(task.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	if forUpdate {
		q = q.ForUpdate()
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return models.TaskModel{}, fmt.Errorf("could not read the task: %w", err)
	}
	if !found {
		return models.TaskModel{}, fmt.Errorf("%w: no such task", apierr.ErrNotFound)
	}
	return taskFrom(row)
}

func (r *taskRepository) List(ctx context.Context) ([]models.TaskModel, error) {
	return r.list(ctx, nil, nil)
}

func (r *taskRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.TaskModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	return r.list(ctx, scoped, nil)
}

// ListOpenByProject returns a project's tasks that are still to be done:
// unclaimed, taken, delegated or raised.
func (r *taskRepository) ListOpenByProject(ctx context.Context, projectID uuid.UUID) ([]models.TaskModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	return r.list(ctx, scoped, []task.Pred{task.Status.In(append([]string{string(models.TaskUnclaimed)}, heldStatuses...)...)})
}

func (r *taskRepository) ListByAssignee(ctx context.Context, assignee string) ([]models.TaskModel, error) {
	return r.list(ctx, nil, []task.Pred{task.Assignee.Eq(assignee)})
}

// ListByInstance returns every task an instance has, newest first.
//
// Every one, not the store's first thousand. The engine acts on what this
// returns — an interrupting deadline withdraws the open tasks of the activity
// it ends, a migration moves the tasks on the nodes it renames — and an
// activity that asks more than a thousand people at once used to leave the
// oldest of them with work the process had abandoned.
func (r *taskRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.TaskModel, error) {
	q, ok, err := r.scopedQuery(ctx, nil, []task.Pred{task.InstanceID.Eq(instanceID)})
	if err != nil || !ok {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	// The id breaks ties, so the cursor is a position: every task one
	// transaction creates carries that transaction's timestamp.
	rows, err := everyRow[task.Row](ctx, ex, q.Order(task.CreatedAt.Desc(), task.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the instance's tasks: %w", err)
	}
	return tasksFrom(rows)
}

// ListByCandidates returns the unclaimed work a person could pick up.
//
// The candidate lists are JSON arrays kept as text, so this matches the quoted
// name in the list's text, as the GORM repository did. Quoted rather than bare
// so "ada" does not match "adam". Read as text, the question holds once the
// columns become jsonb too.
func (r *taskRepository) ListByCandidates(ctx context.Context, c contracts.Candidacy) ([]models.TaskModel, error) {
	q, ok, err := r.candidates(ctx, c)
	if err != nil || !ok {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[task.Row](ctx, ex, q.Order(task.CreatedAt.Desc(), task.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the candidate tasks: %w", err)
	}
	return tasksFrom(rows)
}

func (r *taskRepository) ListByCandidatesPaged(ctx context.Context, c contracts.Candidacy, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	empty := contracts.NewPage([]models.TaskModel{}, 0, p)
	q, ok, err := r.candidates(ctx, c)
	if err != nil || !ok {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count the candidate tasks: %w", err)
	}
	n := p.Normalize()
	// The id breaks ties: the tasks one transaction creates share its
	// created_at, and paging a tie with OFFSET showed some of them twice and
	// others never.
	rows, err := q.
		Order(task.CreatedAt.Desc(), task.ID.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(n.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list the candidate tasks: %w", err)
	}
	items, err := tasksFrom(rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}

// candidates is the unclaimed work in the caller's scope offered to c: to the
// person by name, to one of their groups, or, when c.Unnamed, to nobody.
// ok is false when the scope holds no project.
func (r *taskRepository) candidates(ctx context.Context, c contracts.Candidacy) (task.Query, bool, error) {
	q, ok, err := r.scopedQuery(ctx, nil, []task.Pred{task.Status.Eq(string(models.TaskUnclaimed))})
	if err != nil || !ok {
		return q, ok, err
	}
	offered := []task.Grp{task.And(task.CandidateUsers.TextLikeAny("%" + quoted(c.User) + "%"))}
	if len(c.Groups) > 0 {
		patterns := make([]string, len(c.Groups))
		for i, group := range c.Groups {
			patterns[i] = "%" + quoted(group) + "%"
		}
		offered = append(offered, task.And(task.CandidateGroups.TextLikeAny(patterns...)))
	}
	if c.Unnamed {
		offered = append(offered, namesNobody())
	}
	return q.AnyOf(offered...), true, nil
}

// namesNobody matches a task given to nobody: no assignee, and no candidate
// users or groups (entities.Task.FallsToOperators), whatever kind of task.
//
// This repository writes an empty list as [] and no assignee as NULL. An empty
// assignee, a NULL list, the JSON null and an empty value are what a row
// written some other way can hold, and they name nobody just the same.
func namesNobody() task.Grp {
	return task.And(task.Assignee.IsNullOrEmpty(), task.CandidateUsers.IsEmpty(), task.CandidateGroups.IsEmpty())
}

func (r *taskRepository) ListByAssigneePaged(ctx context.Context, assignee string, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	return r.paged(ctx, nil, []task.Pred{task.Assignee.Eq(assignee)}, p)
}

// ListDelegatedByPaged returns the tasks somebody delegated that are still with
// their delegate.
//
// The three things that make a delegation pending (entities.Task.AwaitsHandBack),
// not the owner alone: a delegated task the engine withdrew, and one that was
// handed back and completed, keep their owner, and a row a pod of the release
// before this one wrote over can keep an owner under a status or a state that
// is no longer a pending delegation. None of those is something its owner is
// still waiting for. Three predicates and the tenant scope are within what the
// store holds per query. Read through ix_tasks_owner, which migration 32 builds.
func (r *taskRepository) ListDelegatedByPaged(ctx context.Context, owner string, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	return r.paged(ctx, nil, []task.Pred{
		task.Owner.Eq(owner),
		task.Status.Eq(string(models.TaskDelegated)),
		task.DelegationState.Eq(string(entities.DelegationPending)),
	}, p)
}

func (r *taskRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return contracts.NewPage([]models.TaskModel{}, 0, p), err
	}
	return r.paged(ctx, scoped, nil, p)
}

func (r *taskRepository) ListByInstancePaged(ctx context.Context, instanceID uuid.UUID, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	return r.paged(ctx, nil, []task.Pred{task.InstanceID.Eq(instanceID)}, p)
}

func (r *taskRepository) ListPaged(ctx context.Context, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	return r.paged(ctx, nil, nil, p)
}

func (r *taskRepository) Create(ctx context.Context, t models.TaskModel) error {
	projectID := uuid.UUID(t.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := task.Create()
	if id := uuid.UUID(t.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(projectID)
	ins.SetInstanceID(uuid.UUID(t.InstanceID))
	ins.SetNodeID(t.NodeID)
	ins.SetName(t.Name)
	ins.SetType(string(t.Type))
	ins.SetStatus(string(t.Status))
	ins.SetPriority(int64(t.Priority))
	setOrNullString(ins.SetDescription, ins.SetDescriptionNull, t.Description)
	setOrNullString(ins.SetAssignee, ins.SetAssigneeNull, t.Assignee)
	setOrNullString(ins.SetOwner, ins.SetOwnerNull, t.Owner)
	setOrNullString(ins.SetDelegationState, ins.SetDelegationStateNull, t.DelegationState)
	setOrNullString(ins.SetFormKey, ins.SetFormKeyNull, t.FormKey)
	setOrNullString(ins.SetFormDefinition, ins.SetFormDefinitionNull, t.FormDefinition)
	setOrNullString(ins.SetIterationID, ins.SetIterationIDNull, t.IterationID)
	if t.DueDate != nil {
		ins.SetDueDate(*t.DueDate)
	}
	if err := writeTaskState(ins.SetCandidateUsers, ins.SetCandidateGroups, ins.SetVariables, t); err != nil {
		return err
	}
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not create the task: %w", err)
	}
	return nil
}

func (r *taskRepository) Update(ctx context.Context, t models.TaskModel) error {
	id := uuid.UUID(t.ID)
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := task.New().Where(task.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the task: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such task", apierr.ErrNotFound)
	}
	mut := task.Mutate(row)
	mut.SetName(t.Name)
	mut.SetNodeID(t.NodeID)
	mut.SetType(string(t.Type))
	mut.SetStatus(string(t.Status))
	mut.SetPriority(int64(t.Priority))
	setOrNullString(mut.SetDescription, mut.SetDescriptionNull, t.Description)
	setOrNullString(mut.SetAssignee, mut.SetAssigneeNull, t.Assignee)
	setOrNullString(mut.SetOwner, mut.SetOwnerNull, t.Owner)
	setOrNullString(mut.SetDelegationState, mut.SetDelegationStateNull, t.DelegationState)
	setOrNullString(mut.SetFormKey, mut.SetFormKeyNull, t.FormKey)
	setOrNullString(mut.SetFormDefinition, mut.SetFormDefinitionNull, t.FormDefinition)
	setOrNullString(mut.SetIterationID, mut.SetIterationIDNull, t.IterationID)
	if t.DueDate != nil {
		mut.SetDueDate(*t.DueDate)
	} else {
		mut.SetDueDateNull()
	}
	if err := writeTaskState(mut.SetCandidateUsers, mut.SetCandidateGroups, mut.SetVariables, t); err != nil {
		return err
	}
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the task: %w", err)
	}
	return nil
}

// UpdateStatus changes only the status, so a claim or a completion does not
// write back a whole task the caller may have read some time ago.
func (r *taskRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status models.TaskStatus) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := task.New().Where(task.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the task: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such task", apierr.ErrNotFound)
	}
	mut := task.Mutate(row)
	mut.SetStatus(string(status))
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the task status: %w", err)
	}
	return nil
}

// RenameFinishedStep changes the step id of a task that is completed or
// canceled, and no other column of it.
//
// One statement whose condition is part of the write, as a job's claim is: a
// finished task is the record of what somebody did, so the caller must not be
// able to put back a status, an assignee or a form it read some time ago, and
// a task that is still open is not this write's to touch. updated_at is left
// alone with the rest: nothing was done to the task, its step was renamed.
func (r *taskRepository) RenameFinishedStep(ctx context.Context, id uuid.UUID, nodeID string) (bool, error) {
	// Scoped as every write of a task is: one the caller cannot see is not there.
	if _, err := r.Get(ctx, id); err != nil {
		return false, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return false, err
	}
	var step task.Mut
	step.SetNodeID(nodeID)
	renamed, err := task.New().Where(
		task.ID.Eq(id),
		task.Status.In(string(models.TaskCompleted), string(models.TaskCanceled)),
	).UpdateAll(ctx, ex, step)
	if err != nil {
		return false, fmt.Errorf("could not rename the finished task's step: %w", err)
	}
	return renamed > 0, nil
}

// CountByStatus counts a project's tasks in one state.
//
// Scoped, and it was not: this counted every organization's work, so the number
// a tenant saw on its dashboard was the whole installation's.
func (r *taskRepository) CountByStatus(ctx context.Context, projectID uuid.UUID, status models.TaskStatus) (int64, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	// An empty status means "any", which is how the dashboard asks for a
	// total. Comparing against the empty string would match nothing and
	// report zero, which reads as a working counter with no work in it.
	q := task.New()
	if status != "" {
		q = q.Where(task.Status.Eq(string(status)))
	}
	if scoped != nil {
		q = q.Where(task.ProjectID.In(uuidsToRaw(scoped)...))
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not count tasks: %w", err)
	}
	return total, nil
}

func (r *taskRepository) list(ctx context.Context, scoped []uuid.UUID, preds []task.Pred) ([]models.TaskModel, error) {
	q, ok, err := r.scopedQuery(ctx, scoped, preds)
	if err != nil || !ok {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Order(task.CreatedAt.Desc()).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list tasks: %w", err)
	}
	return tasksFrom(rows)
}

func (r *taskRepository) paged(ctx context.Context, scoped []uuid.UUID, preds []task.Pred, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	empty := contracts.NewPage([]models.TaskModel{}, 0, p)
	q, ok, err := r.scopedQuery(ctx, scoped, preds)
	if err != nil || !ok {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count tasks: %w", err)
	}
	n := p.Normalize()
	rows, err := q.
		Order(task.CreatedAt.Desc(), task.ID.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(p.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list tasks: %w", err)
	}
	items, err := tasksFrom(rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}

func (r *taskRepository) scopedQuery(ctx context.Context, scoped []uuid.UUID, preds []task.Pred) (task.Query, bool, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return task.Query{}, false, err
	}
	q := task.New().Where(preds...)
	switch {
	case scoped != nil:
		q = q.Where(inProjects(scoped))
	case !scope.unrestricted():
		if len(scope.projects) == 0 {
			return task.Query{}, false, nil
		}
		q = q.Where(inProjects(scope.projects))
	}
	return q, true, nil
}

// inProjects is the tenant predicate: the task belongs to one of these
// projects.
//
// One project is said as an equality rather than as a list of one. The page
// of a project's tasks is read from ix_tasks_project_created (migration 35),
// already in the order it asks for, and only an equality lets the planner use
// that order: `project_id = ANY($1)` is a list of any length to a prepared
// statement, so it reads every matching task and sorts them, as it did before
// the index existed.
func inProjects(ids []uuid.UUID) task.Pred {
	if len(ids) == 1 {
		return task.ProjectID.Eq(ids[0])
	}
	return task.ProjectID.In(uuidsToRaw(ids)...)
}

// quoted renders a name the way it appears inside a JSON array, so a substring
// match cannot pair "ada" with "adam".
func quoted(name string) string {
	encoded, err := json.Marshal(name)
	if err != nil {
		return `"` + name + `"`
	}
	return string(encoded)
}

func writeTaskState(setUsers, setGroups func(runtime.JSON), setVariables func(string), t models.TaskModel) error {
	users, err := json.Marshal(t.CandidateUsers)
	if err != nil {
		return fmt.Errorf("could not encode the task's candidate users: %w", err)
	}
	setUsers(users)
	groups, err := json.Marshal(t.CandidateGroups)
	if err != nil {
		return fmt.Errorf("could not encode the task's candidate groups: %w", err)
	}
	setGroups(groups)

	encrypted, err := t.Variables.Value()
	if err != nil {
		return fmt.Errorf("could not encrypt the task's variables: %w", err)
	}
	setVariables(stringOfValue(encrypted))
	return nil
}

func tasksFrom(rows []task.Row) ([]models.TaskModel, error) {
	out := make([]models.TaskModel, 0, len(rows))
	for _, row := range rows {
		t, err := taskFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func taskFrom(row task.Row) (models.TaskModel, error) {
	t := models.TaskModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:       models.UUID(row.ProjectID),
		InstanceID:      models.UUID(row.InstanceID),
		NodeID:          row.NodeID,
		IterationID:     valueOr(row.IterationID),
		Name:            row.Name,
		Description:     valueOr(row.Description),
		Type:            models.NodeType(row.Type),
		Status:          models.TaskStatus(row.Status),
		Assignee:        valueOr(row.Assignee),
		Owner:           valueOr(row.Owner),
		DelegationState: valueOr(row.DelegationState),
		Priority:        int(row.Priority),
		FormKey:         valueOr(row.FormKey),
		FormDefinition:  valueOr(row.FormDefinition),
	}
	if due, ok := row.DueDate.Get(); ok {
		t.DueDate = &due
	}
	if len(row.CandidateUsers) > 0 {
		if err := json.Unmarshal(row.CandidateUsers, &t.CandidateUsers); err != nil {
			return models.TaskModel{}, fmt.Errorf("could not decode a task's candidate users: %w", err)
		}
	}
	if len(row.CandidateGroups) > 0 {
		if err := json.Unmarshal(row.CandidateGroups, &t.CandidateGroups); err != nil {
			return models.TaskModel{}, fmt.Errorf("could not decode a task's candidate groups: %w", err)
		}
	}
	if row.Variables != "" {
		if err := t.Variables.Scan(row.Variables); err != nil {
			return models.TaskModel{}, fmt.Errorf("could not decrypt a task's variables: %w", err)
		}
	}
	return t, nil
}

// Deadlines reads a project's open tasks that have a due date, soonest first,
// with the process each is part of, and counts all of its open tasks.
//
// The dashboard's deadline report read the first page of the task list: the
// newest 200 tasks of any status, from rows that name no process. Every late
// task read "Unknown process", and in a project with 200 newer tasks the late
// ones were not on the page at all.
func (r *taskRepository) Deadlines(ctx context.Context, projectID uuid.UUID, limit int) ([]contracts.DeadlineRow, contracts.DeadlineCounts, error) {
	var counts contracts.DeadlineCounts
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, counts, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, counts, err
	}
	open := task.New().
		Where(task.Status.NotIn(string(models.TaskCompleted), string(models.TaskCanceled))).
		WhereIf(scoped != nil, task.ProjectID.In(uuidsToRaw(scoped)...))

	counted, err := open.OneDeadlines(ctx, ex)
	if err != nil {
		return nil, counts, fmt.Errorf("could not count the open tasks: %w", err)
	}
	counts.WithDeadline, counts.WithoutDeadline = counted.WithDeadline, counted.WithoutDeadline

	rows, err := open.Where(task.DueDate.IsNotNull()).Limit(int64(limit)).AllDeadline(ctx, ex)
	if err != nil {
		return nil, counts, fmt.Errorf("could not read the open tasks with a deadline: %w", err)
	}
	out := make([]contracts.DeadlineRow, len(rows))
	for i, row := range rows {
		out[i] = contracts.DeadlineRow{
			TaskID:   uuid.UUID(row.ID),
			Name:     row.Name,
			NodeID:   row.NodeID,
			Status:   row.Status,
			Priority: row.Priority,
			Assignee: row.Assignee.V,
			// Milliseconds, as the report has always carried them.
			DueDate:     row.DueDate.V.UTC().Round(time.Millisecond),
			ProcessKey:  row.ProcessKey.V,
			ProcessName: row.ProcessName.V,
		}
	}
	return out, counts, nil
}
