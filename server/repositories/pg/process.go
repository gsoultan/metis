package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/payloads"
	"github.com/gsoultan/metis/server/repositories/store"
	"github.com/gsoultan/metis/server/repositories/store/incident"
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
	// The id is chosen here rather than by the database: a value kept out of
	// the row is kept under it, before the row is written.
	if uuid.UUID(m.ID) == uuid.Nil {
		m.ID = models.UUID(uuid.Must(uuid.NewV7()))
	}
	ins := processinstance.Create()
	ins.SetID(uuid.UUID(m.ID))
	ins.SetProjectID(projectID)
	ins.SetDefinitionID(uuid.UUID(m.DefinitionID))
	ins.SetStatus(string(m.Status))
	if m.ParentInstanceID != nil {
		ins.SetParentInstanceID(uuid.UUID(*m.ParentInstanceID))
	}
	setOrNullString(ins.SetParentNodeID, ins.SetParentNodeIDNull, m.ParentNodeID)
	setOrNullString(ins.SetBusinessKey, ins.SetBusinessKeyNull, m.BusinessKey)
	if err := writeInstanceState(ctx, ins.SetVariables, ins.SetTokens, ins.SetCompletedNodes,
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
	return instanceFrom(ctx, row)
}

// GetForUpdate reads an instance and holds it until the transaction ends.
//
// This is what makes advancing a token safe. Two workers reaching the same
// instance — a job firing while a person completes a task — would otherwise
// both read the same tokens, both decide what happens next, and the second
// write would silently discard the first. The lock makes the second wait.
//
// FOR NO KEY UPDATE rather than FOR UPDATE: an instance is the parent of its
// tasks, notifications, deviations and jobs, and inserting any of them checks
// the instance's key. Under FOR UPDATE that check waits for the lock, so a
// completion holding the instance and waiting for a task deadlocked with a
// hand-over holding the task and notifying somebody about the instance. The
// key never changes, so nothing the stronger lock adds is needed.
func (r *processRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	row, err := r.one(ctx, id, true)
	if err != nil {
		return models.ProcessInstanceModel{}, err
	}
	return instanceFrom(ctx, row)
}

// Update writes an instance's state over the row it names.
//
// The caller's scope is checked by asking whether the row is there for it to
// see, not by reading the row: the update writes every column it needs from m
// and addresses the row by id alone, so the variables and tokens a read would
// fetch, often the largest values in the table, were read only to be dropped.
func (r *processRepository) Update(ctx context.Context, m models.ProcessInstanceModel) error {
	id := uuid.UUID(m.ID)
	if err := r.inScope(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	mut := processinstance.MutateKey(id)
	mut.SetStatus(string(m.Status))
	mut.SetDefinitionID(uuid.UUID(m.DefinitionID))
	if m.ParentInstanceID != nil {
		mut.SetParentInstanceID(uuid.UUID(*m.ParentInstanceID))
	} else {
		mut.SetParentInstanceIDNull()
	}
	setOrNullString(mut.SetParentNodeID, mut.SetParentNodeIDNull, m.ParentNodeID)
	if err := writeInstanceState(ctx, mut.SetVariables, mut.SetTokens, mut.SetCompletedNodes,
		mut.SetCompensatedNodes, mut.SetMultiInstance, mut.SetJoins, m); err != nil {
		return err
	}
	if err := mut.Update(ctx, ex); err != nil {
		// Gone between the check and the write, or never there for a caller
		// whose scope the check did not need to narrow.
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
		}
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
	return instancesFrom(ctx, rows)
}

func (r *processRepository) ListByParent(ctx context.Context, parentInstanceID uuid.UUID) ([]models.ProcessInstanceModel, error) {
	return r.list(ctx, nil, []processinstance.Pred{processinstance.ParentInstanceID.Eq(parentInstanceID)})
}

// instanceScanBatchSize bounds how many instances a scan holds at once: each
// carries its decrypted variables and its tokens.
const instanceScanBatchSize = 100

// ScanByStatus walks every instance in one state, in key order, a batch at a
// time.
//
// For work that must reach all of them, like a one-time repair at upgrade. A
// list stops at the store's thousand rows, and holding every running instance
// at once is bounded by how much work the installation has rather than by
// anything this process chose. The key order stays put while the visitor
// rewrites what it is handed, as long as it leaves the state alone.
func (r *processRepository) ScanByStatus(ctx context.Context, status models.ProcessStatus, visit func([]models.ProcessInstanceModel) error) error {
	q, ok, err := r.scopedQuery(ctx, nil, []processinstance.Pred{processinstance.Status.Eq(string(status))})
	if err != nil || !ok {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	err = everyBatch(ctx, ex, q.Order(processinstance.ID.Asc()), instanceScanBatchSize, func(rows []processinstance.Row) error {
		batch, err := instancesFrom(ctx, rows)
		if err != nil {
			return err
		}
		return visit(batch)
	})
	if err != nil {
		return fmt.Errorf("could not scan the %s instances: %w", status, err)
	}
	return nil
}

func (r *processRepository) ListPageOfTotal(ctx context.Context, projectID uuid.UUID, f contracts.InstanceFilter, p contracts.Pagination, total int64) (contracts.Page[models.ProcessInstanceModel], error) {
	if f.NeedsAttention {
		if projectID == uuid.Nil {
			return r.ListPaged(ctx, f, p)
		}
		return r.ListByProjectPaged(ctx, projectID, f, p)
	}
	var scoped []uuid.UUID
	if projectID != uuid.Nil {
		var visible bool
		var err error
		scoped, visible, err = r.scopedProjects(ctx, projectID)
		if err != nil || !visible {
			return contracts.NewPage([]models.ProcessInstanceModel{}, 0, p), err
		}
	}
	return r.paged(ctx, scoped, f, nil, p, &total)
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

// pagedWithAttention pages the instances, narrowed to those holding an open
// incident when the filter asks for them.
//
// The narrowing is a semi-join, so it reaches every such instance: the list's
// total is the same number the "needs attention" count reports, however many
// there are. It used to name the matching instances first and page over those
// ids, and stopped at a thousand of them.
func (r *processRepository) pagedWithAttention(
	ctx context.Context,
	projectID uuid.UUID,
	scoped []uuid.UUID,
	f contracts.InstanceFilter,
	p contracts.Pagination,
) (contracts.Page[models.ProcessInstanceModel], error) {
	if !f.NeedsAttention {
		return r.paged(ctx, scoped, f, nil, p, nil)
	}
	empty := contracts.NewPage([]models.ProcessInstanceModel{}, 0, p)
	q, ok, err := r.scopedQuery(ctx, scoped, filterPreds(f))
	if err != nil || !ok {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}
	total, err := needingAttention(q).Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count process instances: %w", err)
	}
	if total == 0 || int64(p.Offset()) >= total {
		return contracts.NewPage([]models.ProcessInstanceModel{}, total, p), nil
	}
	n := p.Normalize()
	rows, err := needingAttention(q.
		Order(processinstance.CreatedAt.Desc(), processinstance.ID.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(p.Offset()))).All(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not list process instances: %w", err)
	}
	items, err := instancesFrom(ctx, rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
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
	if f.BusinessKey != "" {
		preds = append(preds, processinstance.BusinessKey.Eq(f.BusinessKey))
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
		q = q.Where(instanceInProjects(scoped))
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

	var scope []processinstance.Pred
	if scoped != nil {
		scope = append(scope, instanceInProjects(scoped))
	}
	open := incident.New().Where(
		incident.Status.Eq(string(models.IncidentOpen)),
		incident.InstanceID.In(uuidsToRaw(instanceIDs)...),
	).Limit(int64(len(instanceIDs))) // a group per instance at most
	groups, err := store.IncidentHavingInstance(open, scope...).AllByInstance(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not count open incidents: %w", err)
	}
	for _, g := range groups {
		counts[uuid.UUID(g.InstanceID)] += g.Total
	}
	return counts, nil
}

// CountInstancesNeedingAttention counts instances holding an open incident.
func (r *processRepository) CountInstancesNeedingAttention(ctx context.Context, projectID uuid.UUID, f contracts.InstanceFilter) (int64, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return 0, err
	}
	q, ok, err := r.scopedQuery(ctx, scoped, filterPreds(contracts.InstanceFilter{DefinitionID: f.DefinitionID, Status: f.Status}))
	if err != nil || !ok {
		return 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	total, err := needingAttention(q).Count(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not count instances needing attention: %w", err)
	}
	return total, nil
}

// CountByStatuses reports how many instances the project holds in each state.
//
// One grouped query rather than one Count per state: the list offers a filter
// chip for every state a project has, and four round trips to draw four numbers
// is three more than the page needs. The grouping is the model's ByStatus
// aggregate; the scope and the filter are predicates on it.
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

	preds := []processinstance.Pred{}
	// nil means "do not filter at all" — only system work asking about every
	// project reaches that. An empty list instead would match no row and report
	// a working counter with nothing in it, which is the opposite answer.
	if scoped != nil {
		preds = append(preds, instanceInProjects(scoped))
	}
	if f.DefinitionID != uuid.Nil {
		preds = append(preds, processinstance.DefinitionID.Eq(f.DefinitionID))
	}
	if f.Status != "" {
		preds = append(preds, processinstance.Status.Eq(string(f.Status)))
	}
	// Every group: there is one per state, and the store's default bound is
	// for rows, not for these.
	groups, err := processinstance.New().Where(preds...).Limit(math.MaxInt64).AllByStatus(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not count process instances by status: %w", err)
	}
	for _, g := range groups {
		counts[models.ProcessStatus(g.Status)] += g.Total
	}
	return counts, nil
}

// CountInstancesByDefinitions reports how much work each version is carrying.
//
// One grouped query rather than one per definition: the version history page
// asks about every version of a key at once, and a query per row is how a list
// of twenty becomes twenty-one round trips. The grouping is the model's
// ByDefinitionAndStatus aggregate.
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

	q := processinstance.New().Where(processinstance.DefinitionID.In(uuidsToRaw(definitionIDs)...))
	if !scope.unrestricted() {
		q = q.Where(instanceInProjects(scope.projects))
	}
	// Every group: at most one per version and state, and the store's default
	// bound is for rows, not for these.
	groups, err := q.Limit(math.MaxInt64).AllByDefinitionAndStatus(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not count instances by definition: %w", err)
	}
	for _, g := range groups {
		count := counts[g.DefinitionID]
		count.Total += g.Total
		switch models.ProcessStatus(g.Status) {
		case models.ProcessActive:
			count.Running += g.Total
		case models.ProcessSuspended:
			count.Suspended += g.Total
		}
		counts[g.DefinitionID] = count
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
		q = q.Where(instanceInProjects(scope.projects))
	}
	if forUpdate {
		q = q.ForNoKeyUpdate()
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

// inScope reports whether the caller may see the instance, reading nothing of
// it. A caller who may see every project is not asked: the write that follows
// finds no row for an id that is not there, and says so.
func (r *processRepository) inScope(ctx context.Context, id uuid.UUID) error {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return err
	}
	if scope.unrestricted() {
		return nil
	}
	if len(scope.projects) == 0 {
		return fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	found, err := processinstance.New().
		Where(processinstance.ID.Eq(id), instanceInProjects(scope.projects)).
		Exists(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the process instance: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
	}
	return nil
}

// instanceInProjects is the tenant predicate: the instance belongs to one of
// these projects.
//
// One project is said as an equality rather than as a list of one. The page
// of a project's instances is read from ix_process_instances_project_created,
// already in the order it asks for, and only an equality lets the planner use
// that order: `project_id = ANY($1)` is a list of any length to a prepared
// statement, so it reads every instance and sorts them. The counts beside the
// page gain the same way. See inProjects for tasks.
func instanceInProjects(ids []uuid.UUID) processinstance.Pred {
	if len(ids) == 1 {
		return processinstance.ProjectID.Eq(ids[0])
	}
	return processinstance.ProjectID.In(uuidsToRaw(ids)...)
}

// list returns every instance matching, newest first.
//
// Every one, not the store's newest thousand: an instance's sub-processes and
// the cases an OCEL export describes are lists nobody pages, and past a
// thousand the oldest fell off both. The id breaks ties in creation time, so
// the cursor is a position.
func (r *processRepository) list(ctx context.Context, scoped []uuid.UUID, preds []processinstance.Pred) ([]models.ProcessInstanceModel, error) {
	q, ok, err := r.scopedQuery(ctx, scoped, preds)
	if err != nil || !ok {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[processinstance.Row](ctx, ex,
		q.Order(processinstance.CreatedAt.Desc(), processinstance.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list process instances: %w", err)
	}
	return instancesFrom(ctx, rows)
}

// paged reads one page. known is the total when the caller counted it already;
// nil counts it here. A known total of nothing, or a page past it, reads no
// rows: there are none to read.
func (r *processRepository) paged(ctx context.Context, scoped []uuid.UUID, f contracts.InstanceFilter, only []uuid.UUID, p contracts.Pagination, known *int64) (contracts.Page[models.ProcessInstanceModel], error) {
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
	var total int64
	if known != nil {
		total = *known
		if int64(p.Offset()) >= total {
			return contracts.NewPage([]models.ProcessInstanceModel{}, total, p), nil
		}
	} else if total, err = q.Count(ctx, ex); err != nil {
		return empty, fmt.Errorf("could not count process instances: %w", err)
	}
	n := p.Normalize()
	rows, err := q.
		Order(processinstance.CreatedAt.Desc(), processinstance.ID.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(p.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list process instances: %w", err)
	}
	items, err := instancesFrom(ctx, rows)
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
		q = q.Where(instanceInProjects(scoped))
	case !scope.unrestricted():
		if len(scope.projects) == 0 {
			return processinstance.Query{}, false, nil
		}
		q = q.Where(instanceInProjects(scope.projects))
	}
	return q, true, nil
}

// writeInstanceState encodes the six columns that carry an instance's state.
//
// Variables go through models.EncryptedMap so the ciphertext is identical to
// what the GORM repository wrote; the rest are ordinary jsonb. A value too
// large for the row is kept in the payload directory first, and the row
// refers to it (see package payloads).
func writeInstanceState(ctx context.Context, setVariables func(string), setTokens, setCompleted, setCompensated, setMulti, setJoins func(runtime.JSON), m models.ProcessInstanceModel) error {
	stored, err := payloads.Offload(ctx, uuid.UUID(m.ID), m.Variables)
	if err != nil {
		return err
	}
	encrypted, err := models.EncryptedMap(stored).Value()
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

func instancesFrom(ctx context.Context, rows []processinstance.Row) ([]models.ProcessInstanceModel, error) {
	out := make([]models.ProcessInstanceModel, 0, len(rows))
	for _, row := range rows {
		instance, err := instanceFrom(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, instance)
	}
	return out, nil
}

func instanceFrom(ctx context.Context, row processinstance.Row) (models.ProcessInstanceModel, error) {
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
		BusinessKey:  valueOr(row.BusinessKey),
	}
	if parent, ok := row.ParentInstanceID.Get(); ok {
		id := models.UUID(parent)
		m.ParentInstanceID = &id
	}
	if row.Variables != "" {
		if err := m.Variables.Scan(row.Variables); err != nil {
			return models.ProcessInstanceModel{}, fmt.Errorf("could not decrypt the instance's variables: %w", err)
		}
		resolved, err := payloads.Resolve(ctx, row.ID, m.Variables)
		if err != nil {
			return models.ProcessInstanceModel{}, fmt.Errorf("could not read instance %s's variables: %w", row.ID, err)
		}
		m.Variables = resolved
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
	q := processinstance.New().Where(processinstance.Status.In(
		string(models.ProcessActive), string(models.ProcessSuspended)))
	if scoped != nil {
		q = q.Where(instanceInProjects(scoped))
	}
	// Every group, in one statement: the store's default bound is for rows.
	groups, err := q.Limit(math.MaxInt64).AllWaitingByStep(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not count the waiting work: %w", err)
	}
	out := make([]contracts.WaitingRow, 0, len(groups))
	for _, g := range groups {
		row := contracts.WaitingRow{
			ProcessKey:  g.Key.V,
			ProcessName: g.Name.V,
			Waiting:     g.Waiting,
			Instances:   g.Instances,
		}
		// A process total has no step; a step row names one, or none when
		// the token carries no node.
		if g.Total == 0 {
			row.NodeID = g.Node.V
		}
		out = append(out, row)
	}
	return out, nil
}
