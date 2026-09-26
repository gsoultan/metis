package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/auditentry"
)

type auditRepository struct{ conn }

// NewAuditRepository returns the account of what happened.
func NewAuditRepository(c *db.Conn) contracts.AuditRepository {
	return &auditRepository{conn{conn: c}}
}

// Create records one entry.
//
// Never scoped and never refused. An audit entry is written on the path of the
// thing it describes, and a write that could fail here would mean an action
// that happened with no record of it — which is worse than a record nobody
// reads.
//
// The entry's place in the trail is never assigned here: the insert names only
// the columns it sets, so seq takes its default, the next number of a sequence,
// in the order this transaction writes.
func (r *auditRepository) Create(ctx context.Context, entry models.AuditModel) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	data, err := sealedJSONOf(entry.Data)
	if err != nil {
		return fmt.Errorf("could not encode the audit entry's data: %w", err)
	}

	ins := auditentry.Create()
	if id := uuid.UUID(entry.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(uuid.UUID(entry.ProjectID))
	ins.SetInstanceID(uuid.UUID(entry.InstanceID))
	ins.SetType(entry.Type)
	ins.SetMessage(entry.Message)
	ins.SetData(data)
	setOrNullString(ins.SetNodeID, ins.SetNodeIDNull, entry.NodeID)
	setOrNullString(ins.SetNodeName, ins.SetNodeNameNull, entry.NodeName)
	setOrNullString(ins.SetNarrative, ins.SetNarrativeNull, entry.Narrative)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not record the audit entry: %w", err)
	}
	return nil
}

// ListByInstance returns one instance's history, oldest first.
func (r *auditRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.AuditModel, error) {
	return r.list(ctx, auditentry.InstanceID.Eq(instanceID))
}

// ListByProject returns a project's history, oldest first.
func (r *auditRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.AuditModel, error) {
	return r.list(ctx, auditentry.ProjectID.Eq(projectID))
}

// list applies the tenant scope and one caller-supplied predicate.
//
// The scope is on project_id, so an entry belonging to another organization is
// absent rather than filtered — an audit trail that could be read across
// tenants is a description of somebody else's business.
//
// Every entry, not the store's oldest thousand: past a thousand the audit view
// stopped a thousand steps in and never reached where the instance is now, the
// execution path stopped at the same place, and the OCEL export lost its most
// recent events.
//
// Oldest first, by created_at and then by seq. created_at is the moment the
// writing transaction began, so every entry one step writes shares it; seq is
// the order that transaction wrote them in, from a sequence. Ordered by
// created_at alone they came back in whatever order PostgreSQL returned the
// ties — where the rows happen to be stored, which a reseal, CLUSTER or
// pg_repack moves — and the ids could not break the tie: they are random, and
// ordering by them started the execution path at the task instead of the start
// event.
//
// Entries written before migration 28 have no seq. They tie as they always did
// and come back as they always have; NULLS FIRST, because every one of them was
// written before any entry that has one.
//
// One statement with the limit lifted, not pg.everyRow: a keyset walk needs an
// order with no ties, and those older entries still have them.
func (r *auditRepository) list(ctx context.Context, pred auditentry.Pred) ([]models.AuditModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}

	q := auditentry.New().Where(pred).Order(auditentry.CreatedAt.Asc(), auditentry.Seq.AscNullsFirst())
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return nil, nil
		}
		q = q.Where(auditentry.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	rows, err := q.Limit(allRows).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the audit trail: %w", err)
	}
	out := make([]models.AuditModel, 0, len(rows))
	for _, row := range rows {
		entry := models.AuditModel{
			Base: models.Base{
				ID:        models.UUID(row.ID),
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
			ProjectID:  models.UUID(row.ProjectID),
			InstanceID: models.UUID(row.InstanceID),
			Type:       row.Type,
			NodeID:     valueOr(row.NodeID),
			NodeName:   valueOr(row.NodeName),
			Message:    row.Message,
			Narrative:  valueOr(row.Narrative),
		}
		if seq, ok := row.Seq.Get(); ok {
			entry.Seq = &seq
		}
		if len(row.Data) > 0 {
			if err := unseal(row.Data, &entry.Data); err != nil {
				return nil, fmt.Errorf("could not decode an audit entry's data: %w", err)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}
