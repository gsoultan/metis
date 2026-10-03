package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
)

type deviationRepository struct{ conn }

// NewDeviationRepository returns the ledger of what was done to an instance
// that its process did not decide.
func NewDeviationRepository(c *db.Conn) contracts.DeviationRepository {
	return &deviationRepository{conn{conn: c}}
}

// Create records one deviation, inside the transaction that makes the change
// it records and nowhere else.
//
// Refused with ErrDeviationOutsideTransaction when none is open: a row written
// on its own connection survives a change that rolled back, or is missing from
// one that committed, and either leaves the ledger saying something that did
// not happen. The first caller who forgets the unit of work fails here, in its
// first test.
//
// Scoped to the caller's organization through the project the row names, and
// the instance has to be in that project: with no foreign key on instance_id
// (see the lock order note in the migration) this read is what stops a row
// pointing a reader at somebody else's case.
//
// before and after hold business variables, so they are sealed like every other
// copy the engine keeps; details is not business data and stays readable in SQL.
func (r *deviationRepository) Create(ctx context.Context, d entities.Deviation) (entities.Deviation, error) {
	if !db.InTransaction(ctx) {
		return entities.Deviation{}, contracts.ErrDeviationOutsideTransaction
	}
	projectID, instanceID, err := r.checkedTarget(ctx, d)
	if err != nil {
		return entities.Deviation{}, err
	}
	if d.ID == uuid.Nil {
		// The database default is a v4: rows written in one transaction share
		// created_at, and ordered by a random id they come back in no order.
		d.ID = uuid.Must(uuid.NewV7())
	}
	ins, err := stageDeviation(d, projectID, instanceID)
	if err != nil {
		return entities.Deviation{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.Deviation{}, err
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.Deviation{}, fmt.Errorf("could not record the deviation: %w", err)
	}
	return deviationFrom(row)
}

// checkedTarget refuses a deviation that is malformed, that names a project
// that is not the caller's, or that names an instance outside that project.
//
// A malformed deviation is a plain error, not apierr.Invalidf: it is made by an
// engine writer, never by a client, so it has to surface as a server error that
// is logged and alerted on, not as a 400 telling the caller about something it
// did not do. The unit of work around it still rolls back.
func (r *deviationRepository) checkedTarget(ctx context.Context, d entities.Deviation) (projectID, instanceID uuid.UUID, err error) {
	if d.Project == nil || d.Project.ID == uuid.Nil || d.Instance == nil || d.Instance.ID == uuid.Nil {
		return uuid.Nil, uuid.Nil, errors.New("deviation: the project and the instance it belongs to are not named")
	}
	if !d.Kind.Valid() || !d.Scope.Valid() || !d.Origin.Valid() || !d.Status.Valid() {
		return uuid.Nil, uuid.Nil, fmt.Errorf("deviation: kind, scope, origin and status must come from the closed sets (got %q, %q, %q, %q)",
			d.Kind, d.Scope, d.Origin, d.Status)
	}
	projectID, instanceID = d.Project.ID, d.Instance.ID
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	found, err := processinstance.New().
		Where(processinstance.ID.Eq(instanceID), processinstance.ProjectID.Eq(projectID)).
		Exists(ctx, ex)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("could not check the process instance: %w", err)
	}
	if !found {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
	}
	return projectID, instanceID, nil
}

// ListByInstance returns every deviation an instance has, oldest first.
//
// Every one, not the store's first thousand, walked with a keyset cursor as the
// incident list is: created_at alone has ties (one act writes several rows in
// one transaction, and created_at is the moment it began), so the id ends the
// order and makes it a position.
func (r *deviationRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]entities.Deviation, error) {
	if err := r.requireInstanceInTenant(ctx, instanceID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[instancedeviation.Row](ctx, ex, instancedeviation.New().
		Where(instancedeviation.InstanceID.Eq(instanceID)).
		Order(instancedeviation.CreatedAt.Asc(), instancedeviation.ID.Asc()))
	if err != nil {
		return nil, fmt.Errorf("could not read the deviations: %w", err)
	}
	out := make([]entities.Deviation, 0, len(rows))
	for _, row := range rows {
		d, err := deviationFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// FindLiveByVisit answers the row of a visit that is applied or awaiting
// approval, if there is one.
//
// Value predicates only, so the bound statement can use the unique index on
// (instance_id, visit_key) whatever plan the server settles on.
func (r *deviationRepository) FindLiveByVisit(ctx context.Context, instanceID uuid.UUID, visitKey string) (entities.Deviation, bool, error) {
	if err := r.requireInstanceInTenant(ctx, instanceID); err != nil {
		return entities.Deviation{}, false, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.Deviation{}, false, err
	}
	row, found, err := instancedeviation.New().
		Where(
			instancedeviation.InstanceID.Eq(instanceID),
			instancedeviation.VisitKey.Eq(visitKey),
			instancedeviation.Status.In(string(entities.DeviationApplied), string(entities.DeviationPendingApproval)),
		).One(ctx, ex)
	if err != nil {
		return entities.Deviation{}, false, fmt.Errorf("could not read the deviation of the visit: %w", err)
	}
	if !found {
		return entities.Deviation{}, false, nil
	}
	d, err := deviationFrom(row)
	if err != nil {
		return entities.Deviation{}, false, err
	}
	return d, true, nil
}
