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
	"github.com/gsoultan/metis/server/repositories/store/batchoperation"
	"github.com/gsoultan/metis/server/repositories/store/batchoperationitem"
	"github.com/gsoultan/storm/runtime"
)

type batchOperationRepository struct{ conn }

// NewBatchOperationRepository returns the store of acts asked for on many
// instances at once.
func NewBatchOperationRepository(c *db.Conn) contracts.BatchOperationRepository {
	return &batchOperationRepository{conn{conn: c}}
}

// matchingInstances is the FROM and WHERE of the instances of a project a
// filter matches, with the arguments it binds, numbered from first.
//
// Raw SQL because the instances are counted, and copied into an operation's
// items, without being read: the store has no INSERT … SELECT, and reading ten
// thousand instances — variables and all — to write their ids back would be
// the whole cost of asking. Every value is a bound argument. The soft-delete
// predicates the store adds to every read are written out.
func matchingInstances(projectID uuid.UUID, filter entities.BatchOperationFilter, first int) (string, []any) {
	args := []any{projectID}
	where := fmt.Sprintf(`FROM process_instances p
	 WHERE p.project_id = $%d AND p.deleted_at IS NULL`, first)
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", first+len(args)-1)
	}
	if filter.Status != "" {
		where += " AND p.status = " + next(filter.Status)
	}
	if filter.DefinitionID != uuid.Nil {
		where += " AND p.definition_id = " + next(filter.DefinitionID)
	}
	if filter.BusinessKey != "" {
		where += " AND p.business_key = " + next(filter.BusinessKey)
	}
	if filter.NeedsAttention {
		where += ` AND EXISTS (SELECT 1 FROM incidents i
		              WHERE i.instance_id = p.id AND i.status = ` + next(string(models.IncidentOpen)) + `
		                AND i.deleted_at IS NULL)`
	}
	return where, args
}

func (r *batchOperationRepository) CountMatching(ctx context.Context, projectID uuid.UUID, filter entities.BatchOperationFilter) (int64, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	where, args := matchingInstances(projectID, filter, 1)
	rows, err := ex.Query(ctx, "SELECT count(*) "+where, args)
	if err != nil {
		return 0, fmt.Errorf("could not count the instances the filter matches: %w", err)
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		if err := scanInt(rows.RawValues(), &count); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
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
	where, args := matchingInstances(operation.ProjectID, operation.Filter, 4)
	insert := `INSERT INTO batch_operation_items (batch_operation_id, instance_id, status, detail)
	           SELECT $1, p.id, $2, '' ` + where + `
	            ORDER BY p.created_at, p.id
	            LIMIT $3`
	total, err := ex.Exec(ctx, insert, append([]any{uuid.UUID(row.ID), string(entities.BatchItemPending), int64(limit)}, args...))
	if err != nil {
		return entities.BatchOperation{}, fmt.Errorf("could not record the batch operation's instances: %w", err)
	}
	if _, err := ex.Exec(ctx, `UPDATE batch_operations SET total = $2 WHERE id = $1`, []any{uuid.UUID(row.ID), total}); err != nil {
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

// stopBatchSQL skips every instance a running operation has not reached and
// marks it stopped, counting what it skipped, in one statement: an instance
// the worker settles at the same moment is either settled or skipped, never
// both, because both take its row.
const stopBatchSQL = `WITH skipped AS (
    UPDATE batch_operation_items
       SET status = $5, detail = $3, settled_at = $4, updated_at = $4
     WHERE batch_operation_id = $1 AND status = $6
 RETURNING 1)
UPDATE batch_operations
   SET status = $7, stopped_by = $2, stop_reason = $3, finished_at = $4, updated_at = $4,
       lease_expires = NULL, skipped = skipped + (SELECT count(*) FROM skipped)
 WHERE id = $1 AND status = $8`

func (r *batchOperationRepository) Stop(ctx context.Context, id uuid.UUID, by, reason string, now time.Time) (entities.BatchOperation, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.BatchOperation{}, err
	}
	if _, err := r.read(ctx, ex, id); err != nil {
		return entities.BatchOperation{}, err
	}
	now = now.UTC()
	if _, err := ex.Exec(ctx, stopBatchSQL, []any{
		id, by, reason, now,
		string(entities.BatchItemSkipped), string(entities.BatchItemPending),
		string(entities.BatchStopped), string(entities.BatchRunning),
	}); err != nil {
		return entities.BatchOperation{}, fmt.Errorf("could not stop the batch operation: %w", err)
	}
	return r.read(ctx, ex, id)
}

// claimBatchSQL takes the oldest running operation whose lease is free. SKIP
// LOCKED, so two workers asking at once take two operations, or one each
// finds nothing, and neither waits.
const claimBatchSQL = `UPDATE batch_operations SET lease_expires = $1
 WHERE id = (
    SELECT id FROM batch_operations
     WHERE status = $3 AND (lease_expires IS NULL OR lease_expires < $2)
     ORDER BY created_at
     LIMIT 1
       FOR UPDATE SKIP LOCKED)
 RETURNING id`

func (r *batchOperationRepository) ClaimRunning(ctx context.Context, now time.Time, lease time.Duration) (entities.BatchOperation, bool, error) {
	if !entities.IsSystemContext(ctx) {
		return entities.BatchOperation{}, false, fmt.Errorf("%w: claiming a batch operation spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.BatchOperation{}, false, err
	}
	now = now.UTC()
	rows, err := ex.Query(ctx, claimBatchSQL, []any{now.Add(lease), now, string(entities.BatchRunning)})
	if err != nil {
		return entities.BatchOperation{}, false, fmt.Errorf("could not claim a batch operation: %w", err)
	}
	var id [16]byte
	found := false
	for rows.Next() {
		values := rows.RawValues()
		if len(values) == 0 || len(values[0]) != len(id) {
			rows.Close()
			return entities.BatchOperation{}, false, fmt.Errorf("could not read a claimed batch operation's id")
		}
		id, found = [16]byte(values[0]), true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return entities.BatchOperation{}, false, fmt.Errorf("could not claim a batch operation: %w", err)
	}
	if !found {
		return entities.BatchOperation{}, false, nil
	}
	row, found, err := batchoperation.New().Where(batchoperation.ID.Eq(id)).One(ctx, ex)
	if err != nil || !found {
		return entities.BatchOperation{}, false, err
	}
	return batchOperationFrom(row), true, nil
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

// settleBatchItemSQL records one instance's result and counts it under its
// status, in one statement, and only once: over an instance still pending, or
// one already skipped when the act did something. The second is a stop that
// came while the instance was being acted on — the stop skips every pending
// instance, this one included, but it was cancelled or retried all the same,
// and the record says what was done rather than that it was not reached. A
// skip never replaces anything, and nothing replaces a success or a failure,
// so no instance is counted twice.
const settleBatchItemSQL = `WITH prior AS (
    SELECT id, status FROM batch_operation_items
     WHERE batch_operation_id = $1 AND instance_id = $2
       AND (status = $6 OR (status = $9 AND $3::text <> $9::text))
       FOR UPDATE),
settled AS (
    UPDATE batch_operation_items i
       SET status = $3, detail = $4, settled_at = $5, updated_at = $5
      FROM prior
     WHERE i.id = prior.id
 RETURNING prior.status AS was, i.status)
UPDATE batch_operations
   SET succeeded = succeeded + (SELECT count(*) FROM settled WHERE status = $7),
       failed    = failed    + (SELECT count(*) FROM settled WHERE status = $8),
       skipped   = skipped   + (SELECT count(*) FROM settled WHERE status = $9)
                             - (SELECT count(*) FROM settled WHERE was = $9),
       updated_at = $5
 WHERE id = $1 AND EXISTS (SELECT 1 FROM settled)`

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
	settled, err := ex.Exec(ctx, settleBatchItemSQL, []any{
		id, item.InstanceID, string(item.Status), item.Detail, at, string(entities.BatchItemPending),
		string(entities.BatchItemSucceeded), string(entities.BatchItemFailed), string(entities.BatchItemSkipped),
	})
	if err != nil {
		return false, fmt.Errorf("could not record what came of an instance of the batch operation: %w", err)
	}
	return settled > 0, nil
}

const finishBatchSQL = `UPDATE batch_operations
   SET status = CASE WHEN EXISTS (
                    SELECT 1 FROM batch_operation_items
                     WHERE batch_operation_id = $1 AND status = $3)
                 THEN status ELSE $4 END,
       finished_at = CASE WHEN EXISTS (
                    SELECT 1 FROM batch_operation_items
                     WHERE batch_operation_id = $1 AND status = $3)
                 THEN finished_at ELSE $2 END,
       lease_expires = NULL, updated_at = $2
 WHERE id = $1 AND status = $5`

func (r *batchOperationRepository) Finish(ctx context.Context, id uuid.UUID, now time.Time) error {
	if !entities.IsSystemContext(ctx) {
		return fmt.Errorf("%w: working through a batch operation spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if _, err := ex.Exec(ctx, finishBatchSQL, []any{
		id, now.UTC(), string(entities.BatchItemPending), string(entities.BatchCompleted), string(entities.BatchRunning),
	}); err != nil {
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
