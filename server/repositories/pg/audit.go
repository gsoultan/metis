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
func (r *auditRepository) list(ctx context.Context, pred auditentry.Pred) ([]models.AuditModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}

	q := auditentry.New().Where(pred).Order(auditentry.CreatedAt.Asc())
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return nil, nil
		}
		q = q.Where(auditentry.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	rows, err := q.All(ctx, ex, nil)
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
		if len(row.Data) > 0 {
			if err := unseal(row.Data, &entry.Data); err != nil {
				return nil, fmt.Errorf("could not decode an audit entry's data: %w", err)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}
