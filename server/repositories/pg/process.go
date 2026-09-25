package pg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/storm/runtime"
)

type processRepository struct{ conn }

// NewProcessRepository returns the store of running processes.
//
// Every row here is a durable business commitment — somebody's purchase order,
// somebody's leave request — so the scoping is on project_id and applies to
// reads and writes alike.
func NewProcessRepository(c *db.Conn) contracts.ProcessRepository {
	return &processRepository{conn{conn: c}}
}

func (r *processRepository) Create(ctx context.Context, m models.ProcessInstanceModel) (uuid.UUID, error) {
	projectID := uuid.UUID(m.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return uuid.Nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	ins := processinstance.Create()
	if id := uuid.UUID(m.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(projectID)
	ins.SetDefinitionID(uuid.UUID(m.DefinitionID))
	ins.SetStatus(string(m.Status))
	if m.ParentInstanceID != nil {
		ins.SetParentInstanceID(uuid.UUID(*m.ParentInstanceID))
	}
	setOrNullString(ins.SetParentNodeID, ins.SetParentNodeIDNull, m.ParentNodeID)
	if err := writeInstanceState(ins.SetVariables, ins.SetTokens, ins.SetCompletedNodes,
		ins.SetCompensatedNodes, ins.SetMultiInstance, ins.SetJoins, m); err != nil {
		return uuid.Nil, err
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not create the process instance: %w", err)
	}
	return row.ID, nil
}

func (r *processRepository) Get(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	row, err := r.one(ctx, id, false)
	if err != nil {
		return models.ProcessInstanceModel{}, err
	}
	return instanceFrom(row)
}

// GetForUpdate reads an instance and holds it until the transaction ends.
//
// This is what makes advancing a token safe. Two workers reaching the same
// instance — a job firing while a person completes a task — would otherwise
// both read the same tokens, both decide what happens next, and the second
// write would silently discard the first. FOR UPDATE makes the second wait.
func (r *processRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	row, err := r.one(ctx, id, true)
	if err != nil {
		return models.ProcessInstanceModel{}, err
	}
	return instanceFrom(row)
}

func (r *processRepository) Update(ctx context.Context, m models.ProcessInstanceModel) error {
	id := uuid.UUID(m.ID)
	row, err := r.one(ctx, id, false)
	if err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	mut := processinstance.Mutate(row)
	mut.SetStatus(string(m.Status))
	mut.SetDefinitionID(uuid.UUID(m.DefinitionID))
	if m.ParentInstanceID != nil {
		mut.SetParentInstanceID(uuid.UUID(*m.ParentInstanceID))
	} else {
		mut.SetParentInstanceIDNull()
	}
	setOrNullString(mut.SetParentNodeID, mut.SetParentNodeIDNull, m.ParentNodeID)
	if err := writeInstanceState(mut.SetVariables, mut.SetTokens, mut.SetCompletedNodes,
		mut.SetCompensatedNodes, mut.SetMultiInstance, mut.SetJoins, m); err != nil {
		return err
	}
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the process instance: %w", err)
	}
	return nil
}

func (r *processRepository) List(ctx context.Context) ([]models.ProcessInstanceModel, error) {
	return r.list(ctx, nil, nil)
}

func (r *processRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ProcessInstanceModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	return r.list(ctx, scoped, nil)
}

// ListByDefinition returns every instance of one version, newest first.
//
// Every one, not the store's first thousand: a migration moves each running
// instance of the version it retires, and one started before a thousand others
// finished used to be outside the window — never moved, and out of reach of a
// second run too, since the finished ones stay on the version.
func (r *processRepository) ListByDefinition(ctx context.Context, definitionID uuid.UUID) ([]models.ProcessInstanceModel, error) {
	q, ok, err := r.scopedQuery(ctx, nil, []processinstance.Pred{processinstance.DefinitionID.Eq(definitionID)})
	if err != nil || !ok {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	// The id breaks ties, so the cursor is a position: two instances can start
	// in the same microsecond.
	rows, err := everyRow[processinstance.Row](ctx, ex,
		q.Order(processinstance.CreatedAt.Desc(), processinstance.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the instances of a version: %w", err)
	}
	return instancesFrom(rows)
}

func (r *processRepository) ListByParent(ctx context.Context, parentInstanceID uuid.UUID) ([]models.ProcessInstanceModel, error) {
	return r.list(ctx, nil, []processinstance.Pred{processinstance.ParentInstanceID.Eq(parentInstanceID)})
}

func (r *processRepository) ListPaged(ctx context.Context, f contracts.InstanceFilter, p contracts.Pagination) (contracts.Page[models.ProcessInstanceModel], error) {
	return r.pagedWithAttention(ctx, uuid.Nil, nil, f, p)
}

func (r *processRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, f contracts.InstanceFilter, p contracts.Pagination) (contracts.Page[models.ProcessInstanceModel], error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return contracts.NewPage([]models.ProcessInstanceModel{}, 0, p), err
	}
	return r.pagedWithAttention(ctx, projectID, scoped, f, p)
}

// pagedWithAttention resolves the "needs attention" filter, then pages.
//
// It is a separate step because "holds an unresolved incident" lives in another
// table, and the generated query builder reaches a correlated subquery only
// through a declared relation — which would put a report in the schema. Naming
// the matching instances is the same answer with a bound on it.
func (r *processRepository) pagedWithAttention(
	ctx context.Context,
	projectID uuid.UUID,
	scoped []uuid.UUID,
	f contracts.InstanceFilter,
	p contracts.Pagination,
) (contracts.Page[models.ProcessInstanceModel], error) {
	if !f.NeedsAttention {
		return r.paged(ctx, scoped, f, nil, p)
	}
	ids, _, err := r.InstancesNeedingAttention(ctx, projectID, f, contracts.AttentionLimit)
	if err != nil {
		return contracts.NewPage([]models.ProcessInstanceModel{}, 0, p), err
	}
	// Nothing needs attention. Short-circuited rather than passed on as an empty
	// IN list, which is a predicate two dialects disagree about and one this
	// build has no reason to emit.
	if len(ids) == 0 {
		return contracts.NewPage([]models.ProcessInstanceModel{}, 0, p), nil
	}
	return r.paged(ctx, scoped, f, ids, p)
}

// filterPreds turns a caller's filter into predicates.
//
// Every value goes through a generated column helper, so each one becomes a
// bound parameter rather than text spliced into SQL — the status arrives from a
// query string.
func filterPreds(f contracts.InstanceFilter) []processinstance.Pred {
	var preds []processinstance.Pred
	if f.Status != "" {
		preds = append(preds, processinstance.Status.Eq(string(f.Status)))
	}
	if f.DefinitionID != uuid.Nil {
		preds = append(preds, processinstance.DefinitionID.Eq(f.DefinitionID))
	}
	return preds
}

// CountByStatus counts a project's instances in one state.
//
// Scoped, and it was not: this counted every organization's work, so the
// dashboard number a tenant saw was the whole installation's.
func (r *processRepository) CountByStatus(ctx context.Context, projectID uuid.UUID, status models.ProcessStatus) (int64, error) {
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
	q := processinstance.New()
	if status != "" {
		q = q.Where(processinstance.Status.Eq(string(status)))
	}
	if scoped != nil {
		q = q.Where(processinstance.ProjectID.In(uuidsToRaw(scoped)...))
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not count process instances: %w", err)
	}
	return total, nil
}

// OpenIncidentsByInstance reports unresolved incidents per instance.
//
// Scoped by joining back to process_instances rather than by trusting the ids
// it was handed: they come from a page this caller just read, but a method that
// is safe only because of who happens to call it is one scope bug away from not
// being.
func (r *processRepository) OpenIncidentsByInstance(ctx context.Context, instanceIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	counts := make(map[uuid.UUID]int64, len(instanceIDs))
	if len(instanceIDs) == 0 {
		return counts, nil
	}
	scoped, visible, err := r.scopedProjects(ctx, uuid.Nil)
	if err != nil || !visible {
		return counts, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}

	query := `SELECT i.instance_id, COUNT(*) AS total
	            FROM incidents i
	            JOIN process_instances p ON p.id = i.instance_id
	           WHERE i.status = $1
	             AND i.deleted_at IS NULL
	             AND p.deleted_at IS NULL
	             AND i.instance_id = ANY($2)`
	args := []any{string(models.IncidentOpen), uuidsToRaw(instanceIDs)}
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		query += fmt.Sprintf(" AND p.project_id = ANY($%d)", len(args))
	}
	query += " GROUP BY i.instance_id"

	rows, err := ex.Query(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("could not count open incidents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 2 {
			continue
		}
		var instanceID uuid.UUID
		copy(instanceID[:], values[0])
		var total int64
		if err := scanInt(values[1:], &total); err != nil {
			return nil, err
		}
		counts[instanceID] += total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not count open incidents: %w", err)
	}
	return counts, nil
}

// CountInstancesNeedingAttention counts instances holding an open incident.
func (r *processRepository) CountInstancesNeedingAttention(ctx context.Context, projectID uuid.UUID, f contracts.InstanceFilter) (int64, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	query, args := needsAttentionQuery("COUNT(DISTINCT i.instance_id)", scoped, f)
	rows, err := ex.Query(ctx, query, args)
	if err != nil {
		return 0, fmt.Errorf("could not count instances needing attention: %w", err)
	}
	defer rows.Close()
	var total int64
	if rows.Next() {
		if err := scanInt(rows.RawValues(), &total); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("could not count instances needing attention: %w", err)
	}
	return total, nil
}

// InstancesNeedingAttention lists the instances holding an open incident.
func (r *processRepository) InstancesNeedingAttention(ctx context.Context, projectID uuid.UUID, f contracts.InstanceFilter, limit int) ([]uuid.UUID, bool, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, true, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, true, err
	}
	query, args := needsAttentionQuery("DISTINCT i.instance_id", scoped, f)
	// One more than asked for, so hitting the bound is detectable rather than
	// indistinguishable from a project that has exactly that many.
	args = append(args, limit+1)
	query += fmt.Sprintf(" LIMIT $%d", len(args))

	rows, err := ex.Query(ctx, query, args)
	if err != nil {
		return nil, true, fmt.Errorf("could not list instances needing attention: %w", err)
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0, limit)
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 1 {
			continue
		}
		var id uuid.UUID
		copy(id[:], values[0])
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("could not list instances needing attention: %w", err)
	}
	if len(ids) > limit {
		return ids[:limit], false, nil
	}
	return ids, true, nil
}

// needsAttentionQuery builds the shared FROM/WHERE for the two reads above.
func needsAttentionQuery(selectList string, scoped []uuid.UUID, f contracts.InstanceFilter) (string, []any) {
	query := `SELECT ` + selectList + `
	            FROM incidents i
	            JOIN process_instances p ON p.id = i.instance_id
	           WHERE i.status = $1
	             AND i.deleted_at IS NULL
	             AND p.deleted_at IS NULL`
	args := []any{string(models.IncidentOpen)}
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		query += fmt.Sprintf(" AND p.project_id = ANY($%d)", len(args))
	}
	if f.DefinitionID != uuid.Nil {
		args = append(args, f.DefinitionID[:])
		query += fmt.Sprintf(" AND p.definition_id = $%d", len(args))
	}
	if f.Status != "" {
		args = append(args, string(f.Status))
		query += fmt.Sprintf(" AND p.status = $%d", len(args))
	}
	return query, args
}

// CountByStatuses reports how many instances the project holds in each state.
//
// One grouped query rather than one Count per state: the list offers a filter
// chip for every state a project has, and four round trips to draw four numbers
// is three more than the page needs. Raw SQL for the same reason as
// CountInstancesByDefinitions — a GROUP BY reaches the generated store only
// through a declared aggregate, and this is a report, not part of the schema.
//
// The whole filter is applied, Status included. A caller drawing one chip per
// state clears it first; see the note on the contract.
func (r *processRepository) CountByStatuses(ctx context.Context, projectID uuid.UUID, f contracts.InstanceFilter) (map[models.ProcessStatus]int64, error) {
	counts := make(map[models.ProcessStatus]int64, len(models.ProcessStatuses))
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return counts, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}

	query := `SELECT status, COUNT(*) AS total FROM process_instances WHERE deleted_at IS NULL`
	args := []any{}
	// nil means "do not filter at all" — only system work asking about every
	// project reaches that. Passing an empty list to `= ANY($1)` instead would
	// match no row and report a working counter with nothing in it, which is the
	// opposite answer.
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		query += fmt.Sprintf(" AND project_id = ANY($%d)", len(args))
	}
	if f.DefinitionID != uuid.Nil {
		args = append(args, f.DefinitionID[:])
		query += fmt.Sprintf(" AND definition_id = $%d", len(args))
	}
	if f.Status != "" {
		args = append(args, string(f.Status))
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	query += " GROUP BY status"

	rows, err := ex.Query(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("could not count process instances by status: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 2 {
			continue
		}
		var total int64
		if err := scanInt(values[1:], &total); err != nil {
			return nil, err
		}
		counts[models.ProcessStatus(values[0])] += total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not count process instances by status: %w", err)
	}
	return counts, nil
}

// CountInstancesByDefinitions reports how much work each version is carrying.
//
// One grouped query rather than one per definition: the version history page
// asks about every version of a key at once, and a query per row is how a list
// of twenty becomes twenty-one round trips. Raw SQL because it is a GROUP BY,
// which the generated store reaches only through a declared aggregate — and
// declaring one in the model layer to serve a single page would put a report in
// the schema.
func (r *processRepository) CountInstancesByDefinitions(ctx context.Context, definitionIDs []uuid.UUID) (map[uuid.UUID]contracts.DefinitionInstanceCount, error) {
	counts := make(map[uuid.UUID]contracts.DefinitionInstanceCount, len(definitionIDs))
	if len(definitionIDs) == 0 {
		return counts, nil
	}
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if !scope.unrestricted() && len(scope.projects) == 0 {
		return counts, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}

	query := `SELECT definition_id, status, COUNT(*) AS total
	            FROM process_instances
	           WHERE deleted_at IS NULL AND definition_id = ANY($1)`
	args := []any{uuidsToRaw(definitionIDs)}
	if !scope.unrestricted() {
		query += " AND project_id = ANY($2)"
		args = append(args, uuidsToRaw(scope.projects))
	}
	query += " GROUP BY definition_id, status"

	rows, err := ex.Query(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("could not count instances by definition: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 3 {
			continue
		}
		var definitionID uuid.UUID
		copy(definitionID[:], values[0])
		var total int64
		if err := scanInt(values[2:], &total); err != nil {
			return nil, err
		}
		count := counts[definitionID]
		count.Total += total
		switch models.ProcessStatus(values[1]) {
		case models.ProcessActive:
			count.Running += total
		case models.ProcessSuspended:
			count.Suspended += total
		}
		counts[definitionID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not count instances by definition: %w", err)
	}
	return counts, nil
}

// one reads an instance within the caller's scope, optionally locking it.
func (r *processRepository) one(ctx context.Context, id uuid.UUID, forUpdate bool) (processinstance.Row, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return processinstance.Row{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return processinstance.Row{}, err
	}
	q := processinstance.New().Where(processinstance.ID.Eq(id))
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return processinstance.Row{}, fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
		}
		q = q.Where(processinstance.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	if forUpdate {
		q = q.ForUpdate()
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return processinstance.Row{}, fmt.Errorf("could not read the process instance: %w", err)
	}
	if !found {
		return processinstance.Row{}, fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
	}
	return row, nil
}

func (r *processRepository) list(ctx context.Context, scoped []uuid.UUID, preds []processinstance.Pred) ([]models.ProcessInstanceModel, error) {
	q, ok, err := r.scopedQuery(ctx, scoped, preds)
	if err != nil || !ok {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Order(processinstance.CreatedAt.Desc()).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list process instances: %w", err)
	}
	return instancesFrom(rows)
}

func (r *processRepository) paged(ctx context.Context, scoped []uuid.UUID, f contracts.InstanceFilter, only []uuid.UUID, p contracts.Pagination) (contracts.Page[models.ProcessInstanceModel], error) {
	empty := contracts.NewPage([]models.ProcessInstanceModel{}, 0, p)
	preds := filterPreds(f)
	if only != nil {
		preds = append(preds, processinstance.ID.In(uuidsToRaw(only)...))
	}
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
		return empty, fmt.Errorf("could not count process instances: %w", err)
	}
	n := p.Normalize()
	rows, err := q.
		Order(processinstance.CreatedAt.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(p.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list process instances: %w", err)
	}
	items, err := instancesFrom(rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}

// scopedQuery builds the tenant filter shared by every list here.
//
// ok is false when the caller can see nothing, which every caller turns into an
// empty result rather than an error.
func (r *processRepository) scopedQuery(ctx context.Context, scoped []uuid.UUID, preds []processinstance.Pred) (processinstance.Query, bool, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return processinstance.Query{}, false, err
	}
	q := processinstance.New().Where(preds...)
	switch {
	case scoped != nil:
		q = q.Where(processinstance.ProjectID.In(uuidsToRaw(scoped)...))
	case !scope.unrestricted():
		if len(scope.projects) == 0 {
			return processinstance.Query{}, false, nil
		}
		q = q.Where(processinstance.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	return q, true, nil
}

// writeInstanceState encodes the six columns that carry an instance's state.
//
// Variables go through models.EncryptedMap so the ciphertext is identical to
// what the GORM repository wrote; the rest are ordinary jsonb.
func writeInstanceState(setVariables func(string), setTokens, setCompleted, setCompensated, setMulti, setJoins func(runtime.JSON), m models.ProcessInstanceModel) error {
	encrypted, err := m.Variables.Value()
	if err != nil {
		return fmt.Errorf("could not encrypt the instance's variables: %w", err)
	}
	setVariables(stringOfValue(encrypted))

	for _, part := range []struct {
		name  string
		value any
		set   func(runtime.JSON)
	}{
		{"tokens", m.Tokens, setTokens},
		{"completed nodes", m.CompletedNodes, setCompleted},
		{"compensated nodes", m.CompensatedNodes, setCompensated},
		{"multi-instance state", m.MultiInstance, setMulti},
		{"joins", m.Joins, setJoins},
	} {
		encoded, err := json.Marshal(part.value)
		if err != nil {
			return fmt.Errorf("could not encode the instance's %s: %w", part.name, err)
		}
		part.set(encoded)
	}
	return nil
}

func instancesFrom(rows []processinstance.Row) ([]models.ProcessInstanceModel, error) {
	out := make([]models.ProcessInstanceModel, 0, len(rows))
	for _, row := range rows {
		instance, err := instanceFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, instance)
	}
	return out, nil
}

func instanceFrom(row processinstance.Row) (models.ProcessInstanceModel, error) {
	m := models.ProcessInstanceModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:    models.UUID(row.ProjectID),
		DefinitionID: models.UUID(row.DefinitionID),
		ParentNodeID: valueOr(row.ParentNodeID),
		Status:       models.ProcessStatus(row.Status),
	}
	if parent, ok := row.ParentInstanceID.Get(); ok {
		id := models.UUID(parent)
		m.ParentInstanceID = &id
	}
	if row.Variables != "" {
		if err := m.Variables.Scan(row.Variables); err != nil {
			return models.ProcessInstanceModel{}, fmt.Errorf("could not decrypt the instance's variables: %w", err)
		}
	}
	for _, part := range []struct {
		name string
		raw  runtime.JSON
		into any
	}{
		{"tokens", row.Tokens, &m.Tokens},
		{"completed nodes", row.CompletedNodes, &m.CompletedNodes},
		{"compensated nodes", row.CompensatedNodes, &m.CompensatedNodes},
		{"multi-instance state", row.MultiInstance, &m.MultiInstance},
		{"joins", row.Joins, &m.Joins},
	} {
		if len(part.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(part.raw, part.into); err != nil {
			return models.ProcessInstanceModel{}, fmt.Errorf("could not decode an instance's %s: %w", part.name, err)
		}
	}
	return m, nil
}

// WaitingByStep counts where a project's running work is sitting, per process
// and step, and — in the rows with no step — how many instances each process
// has running.
//
// One grouped query over the tokens rather than a page of instances sent to
// the browser to count: the dashboard's heat map counted the newest 25, so at
// volume the longest-stuck work was the first to fall off it.
func (r *processRepository) WaitingByStep(ctx context.Context, projectID uuid.UUID) ([]contracts.WaitingRow, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT d.key,
	                 (array_agg(d.name ORDER BY d.version DESC))[1],
	                 COALESCE(t->>'node_id', ''),
	                 count(*),
	                 count(DISTINCT p.id),
	                 GROUPING(t->>'node_id')
	            FROM process_instances p
	            JOIN process_definitions d ON d.id = p.definition_id
	           CROSS JOIN LATERAL jsonb_array_elements(
	                   CASE WHEN jsonb_typeof(p.tokens::jsonb) = 'array' THEN p.tokens::jsonb ELSE '[]'::jsonb END) AS t
	           WHERE p.deleted_at IS NULL
	             AND p.status IN ($1, $2)
	             AND COALESCE(t->>'status', '') <> $3`
	args := []any{string(models.ProcessActive), string(models.ProcessSuspended), string(models.TokenCompleted)}
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		query += fmt.Sprintf(" AND p.project_id = ANY($%d)", len(args))
	}
	query += ` GROUP BY GROUPING SETS ((d.key, t->>'node_id'), (d.key))`

	rows, err := ex.Query(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("could not count the waiting work: %w", err)
	}
	defer rows.Close()
	var out []contracts.WaitingRow
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 6 {
			continue
		}
		var row contracts.WaitingRow
		var grouped int64
		row.ProcessKey, row.ProcessName = string(values[0]), string(values[1])
		if err := scanInt(values[3:4], &row.Waiting); err != nil {
			return nil, err
		}
		if err := scanInt(values[4:5], &row.Instances); err != nil {
			return nil, err
		}
		if err := scanInt(values[5:6], &grouped); err != nil {
			return nil, err
		}
		// A process total has no step; a step row names one.
		if grouped == 0 {
			row.NodeID = string(values[2])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not count the waiting work: %w", err)
	}
	return out, nil
}
