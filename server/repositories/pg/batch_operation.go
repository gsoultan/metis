package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store"
	"github.com/gsoultan/metis/server/repositories/store/batchoperation"
	"github.com/gsoultan/metis/server/repositories/store/batchoperationitem"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/storm/runtime"
)

type batchOperationRepository struct{ conn }

// NewBatchOperationRepository returns the store of acts asked for on many
// instances at once.
func NewBatchOperationRepository(c *db.Conn) contracts.BatchOperationRepository {
	return &batchOperationRepository{conn{conn: c}}
}

// matching is the instances of a project a filter matches, as a read of the
// instance table, and whether the filter asks for only the ones that need
// attention: those are the ones with an open incident, a semi-join the read
// is narrowed by where it is used.
func matching(projectID uuid.UUID, filter entities.BatchOperationFilter) (processinstance.Query, bool) {
	q := processinstance.New().Where(processinstance.ProjectID.Eq(projectID))
	if filter.Status != "" {
		q = q.Where(processinstance.Status.Eq(filter.Status))
	}
	if filter.DefinitionID != uuid.Nil {
		q = q.Where(processinstance.DefinitionID.Eq(filter.DefinitionID))
	}
	if filter.BusinessKey != "" {
		q = q.Where(processinstance.BusinessKey.Eq(filter.BusinessKey))
	}
	return q, filter.NeedsAttention
}

// needingAttention narrows q to the instances with an open incident.
func needingAttention(q processinstance.Query) store.ProcessInstanceIncidentsProbeQuery {
	return store.ProcessInstanceHavingIncidents(q, incident.Status.Eq(string(models.IncidentOpen)))
}

func (r *batchOperationRepository) CountMatching(ctx context.Context, projectID uuid.UUID, filter entities.BatchOperationFilter) (int64, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	q, attention := matching(projectID, filter)
	var count int64
	if attention {
		count, err = needingAttention(q).Count(ctx, ex)
	} else {
		count, err = q.Count(ctx, ex)
	}
	if err != nil {
		return 0, fmt.Errorf("could not count the instances the filter matches: %w", err)
	}
	return count, nil
}

func (r *batchOperationRepository) CountRunning(ctx context.Context, projectID uuid.UUID) (int64, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	n, err := batchoperation.New().
		Where(batchoperation.ProjectID.Eq(projectID), batchoperation.Status.Eq(string(entities.BatchRunning))).
		Count(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not count the project's running batch operations: %w", err)
	}
	return n, nil
}

// Create writes the operation, then copies the instances its filter matches
// into its items in one statement, oldest first, and records how many there
// were. The instances are fixed here: one started after this is not reached,
// and one that no longer matches when the worker gets to it is acted on as it
// then stands.
func (r *batchOperationRepository) Create(ctx context.Context, operation entities.BatchOperation, limit int) (entities.BatchOperation, error) {
	if !db.InTransaction(ctx) {
		return entities.BatchOperation{}, contracts.ErrBatchOperationOutsideTransaction
	}
	if err := r.requireProjectInTenant(ctx, operation.ProjectID); err != nil {
		return entities.BatchOperation{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.BatchOperation{}, err
	}
	operation.Status = entities.BatchRunning
	ins := stageBatchOperation(operation)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.BatchOperation{}, fmt.Errorf("could not record the batch operation: %w", err)
	}
	items := batchoperationitem.Create()
	items.SetBatchOperationID(row.ID)
	items.SetStatus(string(entities.BatchItemPending))
	items.SetDetail("")
	q, attention := matching(operation.ProjectID, operation.Filter)
	q = q.Order(processinstance.CreatedAt.Asc(), processinstance.ID.Asc()).Limit(int64(limit))
	var total int64
	if attention {
		total, err = needingAttention(q).InsertBatchOperationItemPerInstance(ctx, ex, items)
	} else {
		total, err = store.InsertBatchOperationItemPerInstance(ctx, ex, items, q)
	}
	if err != nil {
		return entities.BatchOperation{}, fmt.Errorf("could not record the batch operation's instances: %w", err)
	}
	var counted batchoperation.Mut
	counted.SetTotal(total)
	if _, err := batchoperation.New().Where(batchoperation.ID.Eq(row.ID)).UpdateAll(ctx, ex, counted); err != nil {
		return entities.BatchOperation{}, fmt.Errorf("could not record how many instances the batch operation has: %w", err)
	}
	row.Total = total
	return batchOperationFrom(row), nil
}

func (r *batchOperationRepository) Get(ctx context.Context, id uuid.UUID) (entities.BatchOperation, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.BatchOperation{}, err
	}
	return r.read(ctx, ex, id)
}

// read reads one operation of the caller's; one of somebody else's project is
// not found, as one that never existed.
func (r *batchOperationRepository) read(ctx context.Context, ex runtime.Executor, id uuid.UUID) (entities.BatchOperation, error) {
	row, found, err := batchoperation.New().Where(batchoperation.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return entities.BatchOperation{}, fmt.Errorf("could not read the batch operation: %w", err)
	}
	if !found || r.requireProjectInTenant(ctx, row.ProjectID) != nil {
		return entities.BatchOperation{}, fmt.Errorf("%w: no such batch operation", apierr.ErrNotFound)
	}
	return batchOperationFrom(row), nil
}

func (r *batchOperationRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]entities.BatchOperation, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := batchoperation.New().
		Where(batchoperation.ProjectID.Eq(projectID)).
		Order(batchoperation.CreatedAt.Desc(), batchoperation.ID.Desc()).
		Limit(limit).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the project's batch operations: %w", err)
	}
	out := make([]entities.BatchOperation, 0, len(rows))
	for _, row := range rows {
		out = append(out, batchOperationFrom(row))
	}
	return out, nil
}

func (r *batchOperationRepository) Items(ctx context.Context, id uuid.UUID, status entities.BatchItemStatus, page contracts.Pagination) (contracts.Page[entities.BatchItem], error) {
	empty := contracts.NewPage([]entities.BatchItem{}, 0, page)
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}
	if _, err := r.read(ctx, ex, id); err != nil {
		return empty, err
	}
	q := batchoperationitem.New().Where(batchoperationitem.BatchOperationID.Eq(id))
	if status != "" {
		q = q.Where(batchoperationitem.Status.Eq(string(status)))
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count the batch operation's instances: %w", err)
	}
	n := page.Normalize()
	rows, err := q.Order(batchoperationitem.InstanceID.Asc()).
		Limit(int64(n.PageSize)).
		Offset(int64(page.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list the batch operation's instances: %w", err)
	}
	items := make([]entities.BatchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, batchItemFrom(row))
	}
	return contracts.NewPage(items, total, page), nil
}

// Stop skips every instance a running operation has not reached and marks it
// stopped, counting what it skipped, in one statement: an instance the worker
// settles at the same moment is either settled or skipped, never both,
// because both take its row.
func (r *batchOperationRepository) Stop(ctx context.Context, id uuid.UUID, by, reason string, now time.Time) (entities.BatchOperation, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.BatchOperation{}, err
	}
	if _, err := r.read(ctx, ex, id); err != nil {
		return entities.BatchOperation{}, err
	}
	now = now.UTC()
	var skip batchoperationitem.Mut
	skip.SetStatus(string(entities.BatchItemSkipped))
	skip.SetDetail(reason)
	skip.SetSettledAt(now)
	skip.SetUpdatedAt(now)
	var stop batchoperation.Mut
	stop.SetStatus(string(entities.BatchStopped))
	stop.SetStoppedBy(by)
	stop.SetStopReason(reason)
	stop.SetFinishedAt(now)
	stop.SetUpdatedAt(now)
	stop.SetLeaseExpiresNull()
	var skipped batchoperation.Tally
	skipped.AddSkipped()
	if _, err := batchoperation.New().
		Where(batchoperation.ID.Eq(id), batchoperation.Status.Eq(string(entities.BatchRunning))).
		UpdateAllAfter(ctx, ex, pendingItems(id).UpdateAllStep(skip), stop, skipped); err != nil {
		return entities.BatchOperation{}, fmt.Errorf("could not stop the batch operation: %w", err)
	}
	return r.read(ctx, ex, id)
}

// pendingItems is the instances an operation has not reached yet.
func pendingItems(id uuid.UUID) batchoperationitem.Query {
	return batchoperationitem.New().Where(
		batchoperationitem.BatchOperationID.Eq(id),
		batchoperationitem.Status.Eq(string(entities.BatchItemPending)))
}

// ClaimRunning takes the oldest running operation whose lease is free. SKIP
// LOCKED, so two workers asking at once take two operations, or one each
// finds nothing, and neither waits.
func (r *batchOperationRepository) ClaimRunning(ctx context.Context, now time.Time, lease time.Duration) (entities.BatchOperation, bool, error) {
	if !entities.IsSystemContext(ctx) {
		return entities.BatchOperation{}, false, fmt.Errorf("%w: claiming a batch operation spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.BatchOperation{}, false, err
	}
	now = now.UTC()
	var leased batchoperation.Mut
	leased.SetLeaseExpires(now.Add(lease))
	rows, err := batchoperation.New().
		Where(batchoperation.Status.Eq(string(entities.BatchRunning))).
		Any(batchoperation.LeaseExpires.IsNull(), batchoperation.LeaseExpires.Lt(now)).
		Order(batchoperation.CreatedAt.Asc()).
		Limit(1).
		ForUpdateSkipLocked().
		Take(ctx, ex, leased, nil)
	if err != nil {
		return entities.BatchOperation{}, false, fmt.Errorf("could not claim a batch operation: %w", err)
	}
	if len(rows) == 0 {
		return entities.BatchOperation{}, false, nil
	}
	return batchOperationFrom(rows[0]), true, nil
}

func (r *batchOperationRepository) PendingInstances(ctx context.Context, id uuid.UUID, limit int) ([]uuid.UUID, error) {
	if !entities.IsSystemContext(ctx) {
		return nil, fmt.Errorf("%w: working through a batch operation spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := batchoperationitem.New().
		Where(batchoperationitem.BatchOperationID.Eq(id), batchoperationitem.Status.Eq(string(entities.BatchItemPending))).
		Order(batchoperationitem.InstanceID.Asc()).
		Limit(int64(limit)).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read what the batch operation has left: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.InstanceID)
	}
	return ids, nil
}

// Settle records one instance's result and counts it under its status, and
// only once: over an instance still pending, or one already skipped when the
// act did something. The second is a stop that came while the instance was
// being acted on — the stop skips every pending instance, this one included,
// but it was cancelled or retried all the same, and the record says what was
// done rather than that it was not reached. A skip never replaces anything,
// and nothing replaces a success or a failure, so no instance is counted
// twice.
//
// Each attempt is one statement that records the result and moves the
// operation's counts together. The second runs only when the first found the
// instance no longer pending, which is the race with a stop.
func (r *batchOperationRepository) Settle(ctx context.Context, id uuid.UUID, item entities.BatchItem) (bool, error) {
	if !entities.IsSystemContext(ctx) {
		return false, fmt.Errorf("%w: working through a batch operation spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return false, err
	}
	at := time.Now().UTC()
	if item.SettledAt != nil {
		at = item.SettledAt.UTC()
	}
	settled, err := settleItem(ctx, ex, id, item, at, entities.BatchItemPending)
	if err == nil && settled == 0 && item.Status != entities.BatchItemSkipped {
		settled, err = settleItem(ctx, ex, id, item, at, entities.BatchItemSkipped)
	}
	if err != nil {
		return false, fmt.Errorf("could not record what came of an instance of the batch operation: %w", err)
	}
	return settled > 0, nil
}

// settleItem records item over an instance that is still in status was, and
// moves the operation's counts by what it recorded: one more under the
// item's status, and one fewer skipped when it was skipped before.
func settleItem(ctx context.Context, ex runtime.Executor, id uuid.UUID, item entities.BatchItem, at time.Time, was entities.BatchItemStatus) (int64, error) {
	var result batchoperationitem.Mut
	result.SetStatus(string(item.Status))
	result.SetDetail(item.Detail)
	result.SetSettledAt(at)
	result.SetUpdatedAt(at)
	var touched batchoperation.Mut
	touched.SetUpdatedAt(at)
	var counts batchoperation.Tally
	switch item.Status {
	case entities.BatchItemSucceeded:
		counts.AddSucceeded()
	case entities.BatchItemFailed:
		counts.AddFailed()
	case entities.BatchItemSkipped:
		counts.AddSkipped()
	}
	if was == entities.BatchItemSkipped {
		counts.SubtractSkipped()
	}
	counts.OnlyIfAny()
	step := batchoperationitem.New().Where(
		batchoperationitem.BatchOperationID.Eq(id),
		batchoperationitem.InstanceID.Eq(item.InstanceID),
		batchoperationitem.Status.Eq(string(was))).
		UpdateAllStep(result)
	return batchoperation.New().Where(batchoperation.ID.Eq(id)).UpdateAllAfter(ctx, ex, step, touched, counts)
}

// Finish releases a running operation's lease and, when none of its
// instances is pending any more, completes it, in one statement.
func (r *batchOperationRepository) Finish(ctx context.Context, id uuid.UUID, now time.Time) error {
	if !entities.IsSystemContext(ctx) {
		return fmt.Errorf("%w: working through a batch operation spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	now = now.UTC()
	var released batchoperation.Mut
	released.SetLeaseExpiresNull()
	released.SetUpdatedAt(now)
	var done batchoperation.Tally
	done.SetStatusIfNone(string(entities.BatchCompleted))
	done.SetFinishedAtIfNone(now)
	if _, err := batchoperation.New().
		Where(batchoperation.ID.Eq(id), batchoperation.Status.Eq(string(entities.BatchRunning))).
		UpdateAllAfter(ctx, ex, pendingItems(id).ExistsStep(), released, done); err != nil {
		return fmt.Errorf("could not finish the batch operation: %w", err)
	}
	return nil
}

// stageBatchOperation sets every column of a new operation. Its id is a v7:
// the list orders ties in creation time by id.
func stageBatchOperation(operation entities.BatchOperation) batchoperation.Ins {
	id := operation.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	ins := batchoperation.Create()
	ins.SetID(id)
	ins.SetProjectID(operation.ProjectID)
	ins.SetOrganizationID(operation.OrganizationID)
	ins.SetKind(string(operation.Kind))
	ins.SetReason(operation.Reason)
	ins.SetFilterStatus(operation.Filter.Status)
	if operation.Filter.DefinitionID != uuid.Nil {
		ins.SetFilterDefinitionID(operation.Filter.DefinitionID)
	} else {
		ins.SetFilterDefinitionIDNull()
	}
	ins.SetFilterBusinessKey(operation.Filter.BusinessKey)
	ins.SetFilterNeedsAttention(operation.Filter.NeedsAttention)
	ins.SetRequestedBy(operation.RequestedBy)
	ins.SetRequestedByID(operation.RequestedByID)
	ins.SetStatus(string(operation.Status))
	ins.SetTotal(0)
	ins.SetSucceeded(0)
	ins.SetFailed(0)
	ins.SetSkipped(0)
	ins.SetLeaseExpiresNull()
	ins.SetFinishedAtNull()
	ins.SetStoppedBy("")
	ins.SetStopReason("")
	return ins
}

func batchOperationFrom(row batchoperation.Row) entities.BatchOperation {
	operation := entities.BatchOperation{
		ID:             row.ID,
		CreatedAt:      row.CreatedAt,
		ProjectID:      row.ProjectID,
		OrganizationID: row.OrganizationID,
		Kind:           entities.BatchOperationKind(row.Kind),
		Reason:         row.Reason,
		Filter: entities.BatchOperationFilter{
			Status:         row.FilterStatus,
			BusinessKey:    row.FilterBusinessKey,
			NeedsAttention: row.FilterNeedsAttention,
		},
		RequestedBy:   row.RequestedBy,
		RequestedByID: row.RequestedByID,
		Status:        entities.BatchOperationStatus(row.Status),
		Total:         row.Total,
		Succeeded:     row.Succeeded,
		Failed:        row.Failed,
		Skipped:       row.Skipped,
		StoppedBy:     row.StoppedBy,
		StopReason:    row.StopReason,
	}
	if definition, ok := row.FilterDefinitionID.Get(); ok {
		operation.Filter.DefinitionID = definition
	}
	if finished, ok := row.FinishedAt.Get(); ok {
		operation.FinishedAt = &finished
	}
	return operation
}

func batchItemFrom(row batchoperationitem.Row) entities.BatchItem {
	item := entities.BatchItem{
		InstanceID: row.InstanceID,
		Status:     entities.BatchItemStatus(row.Status),
		Detail:     row.Detail,
	}
	if settled, ok := row.SettledAt.Get(); ok {
		item.SettledAt = &settled
	}
	return item
}
