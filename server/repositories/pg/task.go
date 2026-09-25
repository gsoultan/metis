package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
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

func (r *taskRepository) ListByAssignee(ctx context.Context, assignee string) ([]models.TaskModel, error) {
	return r.list(ctx, nil, []task.Pred{task.Assignee.Eq(assignee)})
}

func (r *taskRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.TaskModel, error) {
	return r.list(ctx, nil, []task.Pred{task.InstanceID.Eq(instanceID)})
}

// ListWithFilters is the operational task list.
func (r *taskRepository) ListWithFilters(ctx context.Context, filter contracts.TaskFilter) ([]models.TaskModel, error) {
	var scoped []uuid.UUID
	if filter.ProjectID != nil {
		var visible bool
		var err error
		scoped, visible, err = r.scopedProjects(ctx, *filter.ProjectID)
		if err != nil || !visible {
			return nil, err
		}
	}
	preds := make([]task.Pred, 0, 3)
	if len(filter.Status) > 0 {
		statuses := make([]string, 0, len(filter.Status))
		for _, status := range filter.Status {
			statuses = append(statuses, string(status))
		}
		preds = append(preds, task.Status.In(statuses...))
	}
	if filter.Assignee != nil {
		preds = append(preds, task.Assignee.Eq(*filter.Assignee))
	}
	if filter.Priority != nil {
		preds = append(preds, task.Priority.Eq(int64(*filter.Priority)))
	}
	return r.list(ctx, scoped, preds)
}

// ListByCandidates returns the unclaimed work a person could pick up.
//
// The candidate lists are JSON arrays in a text column, so this matches the
// quoted name as a substring — the same predicate the GORM repository used, and
// the same one this becomes `jsonb ?| array[...]` when the column types are
// reconciled at the end of the port. Quoted rather than bare so "ada" does not
// match "adam".
//
// Raw SQL because the predicate is a disjunction over a variable number of
// groups, which the generated store expresses only for a fixed column.
func (r *taskRepository) ListByCandidates(ctx context.Context, userID string, groups []string) ([]models.TaskModel, error) {
	rows, _, err := r.candidates(ctx, userID, groups, nil)
	return rows, err
}

func (r *taskRepository) ListByCandidatesPaged(ctx context.Context, userID string, groups []string, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	n := p.Normalize()
	items, total, err := r.candidates(ctx, userID, groups, &n)
	if err != nil {
		return contracts.NewPage([]models.TaskModel{}, 0, p), err
	}
	return contracts.NewPage(items, total, p), nil
}

func (r *taskRepository) candidates(ctx context.Context, userID string, groups []string, page *contracts.Pagination) ([]models.TaskModel, int64, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	if !scope.unrestricted() && len(scope.projects) == 0 {
		return nil, 0, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, 0, err
	}

	where := []string{"deleted_at IS NULL", "status = $1"}
	args := []any{string(models.TaskUnclaimed)}

	matches := []string{fmt.Sprintf("candidate_users LIKE $%d", len(args)+1)}
	args = append(args, "%"+quoted(userID)+"%")
	for _, group := range groups {
		matches = append(matches, fmt.Sprintf("candidate_groups LIKE $%d", len(args)+1))
		args = append(args, "%"+quoted(group)+"%")
	}
	where = append(where, "("+strings.Join(matches, " OR ")+")")

	if !scope.unrestricted() {
		where = append(where, fmt.Sprintf("project_id = ANY($%d)", len(args)+1))
		args = append(args, uuidsToRaw(scope.projects))
	}
	predicate := strings.Join(where, " AND ")

	var total int64
	countRows, err := ex.Query(ctx, "SELECT COUNT(*) FROM tasks WHERE "+predicate, args)
	if err != nil {
		return nil, 0, fmt.Errorf("could not count the candidate tasks: %w", err)
	}
	if countRows.Next() {
		if err := scanInt(countRows.RawValues(), &total); err != nil {
			countRows.Close()
			return nil, 0, err
		}
	}
	countRows.Close()
	if err := countRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("could not count the candidate tasks: %w", err)
	}

	query := "SELECT id FROM tasks WHERE " + predicate + " ORDER BY created_at DESC"
	if page != nil {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
		args = append(args, page.PageSize, page.Offset())
	}
	idRows, err := ex.Query(ctx, query, args)
	if err != nil {
		return nil, 0, fmt.Errorf("could not list the candidate tasks: %w", err)
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
		return nil, 0, fmt.Errorf("could not list the candidate tasks: %w", err)
	}
	if len(ids) == 0 {
		return nil, total, nil
	}

	// The rows themselves come back through the generated reader, so the
	// decoding — including the encrypted variables — is the same code every
	// other read here uses.
	rows, err := task.New().
		Where(task.ID.In(ids...)).
		Order(task.CreatedAt.Desc()).
		Limit(int64(len(ids))).
		All(ctx, ex, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("could not read the candidate tasks: %w", err)
	}
	items, err := tasksFrom(rows)
	return items, total, err
}

func (r *taskRepository) ListByAssigneePaged(ctx context.Context, assignee string, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	return r.paged(ctx, nil, []task.Pred{task.Assignee.Eq(assignee)}, p)
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
	setOrNullString(ins.SetFormKey, ins.SetFormKeyNull, t.FormKey)
	setOrNullString(ins.SetFormDefinition, ins.SetFormDefinitionNull, t.FormDefinition)
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
	setOrNullString(mut.SetFormKey, mut.SetFormKeyNull, t.FormKey)
	setOrNullString(mut.SetFormDefinition, mut.SetFormDefinitionNull, t.FormDefinition)
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
		Order(task.CreatedAt.Desc()).
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
		q = q.Where(task.ProjectID.In(uuidsToRaw(scoped)...))
	case !scope.unrestricted():
		if len(scope.projects) == 0 {
			return task.Query{}, false, nil
		}
		q = q.Where(task.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	return q, true, nil
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
		ProjectID:      models.UUID(row.ProjectID),
		InstanceID:     models.UUID(row.InstanceID),
		NodeID:         row.NodeID,
		Name:           row.Name,
		Description:    valueOr(row.Description),
		Type:           models.NodeType(row.Type),
		Status:         models.TaskStatus(row.Status),
		Assignee:       valueOr(row.Assignee),
		Priority:       int(row.Priority),
		FormKey:        valueOr(row.FormKey),
		FormDefinition: valueOr(row.FormDefinition),
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
	args := []any{string(models.TaskCompleted), string(models.TaskCanceled)}
	open := `t.deleted_at IS NULL AND t.status <> $1 AND t.status <> $2`
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		open += fmt.Sprintf(" AND t.project_id = ANY($%d)", len(args))
	}

	counted, err := ex.Query(ctx, `SELECT count(*) FILTER (WHERE t.due_date IS NOT NULL),
	                                      count(*) FILTER (WHERE t.due_date IS NULL)
	                                 FROM tasks t WHERE `+open, args)
	if err != nil {
		return nil, counts, fmt.Errorf("could not count the open tasks: %w", err)
	}
	for counted.Next() {
		values := counted.RawValues()
		if len(values) < 2 {
			continue
		}
		if err := scanInt(values[0:1], &counts.WithDeadline); err != nil {
			counted.Close()
			return nil, counts, err
		}
		if err := scanInt(values[1:2], &counts.WithoutDeadline); err != nil {
			counted.Close()
			return nil, counts, err
		}
	}
	counted.Close()
	if err := counted.Err(); err != nil {
		return nil, counts, fmt.Errorf("could not count the open tasks: %w", err)
	}

	listArgs := append(slices.Clone(args), limit)
	rows, err := ex.Query(ctx, `SELECT t.id, t.name, t.node_id, t.status, t.priority::int8, COALESCE(t.assignee, ''),
	                                   (extract(epoch FROM t.due_date) * 1000)::int8,
	                                   COALESCE(d.key, ''), COALESCE(d.name, '')
	                              FROM tasks t
	                              LEFT JOIN process_instances p ON p.id = t.instance_id
	                              LEFT JOIN process_definitions d ON d.id = p.definition_id
	                             WHERE `+open+` AND t.due_date IS NOT NULL
	                             ORDER BY t.due_date, t.id
	                             LIMIT $`+fmt.Sprint(len(listArgs)), listArgs)
	if err != nil {
		return nil, counts, fmt.Errorf("could not read the open tasks with a deadline: %w", err)
	}
	defer rows.Close()
	var out []contracts.DeadlineRow
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 9 {
			continue
		}
		var row contracts.DeadlineRow
		if row.TaskID, err = uuid.FromBytes(values[0]); err != nil {
			return nil, counts, fmt.Errorf("could not read a task id: %w", err)
		}
		row.Name, row.NodeID, row.Status = string(values[1]), string(values[2]), string(values[3])
		if err := scanInt(values[4:5], &row.Priority); err != nil {
			return nil, counts, err
		}
		row.Assignee = string(values[5])
		var dueMillis int64
		if err := scanInt(values[6:7], &dueMillis); err != nil {
			return nil, counts, err
		}
		row.DueDate = time.UnixMilli(dueMillis).UTC()
		row.ProcessKey, row.ProcessName = string(values[7]), string(values[8])
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, counts, fmt.Errorf("could not read the open tasks with a deadline: %w", err)
	}
	return out, counts, nil
}
