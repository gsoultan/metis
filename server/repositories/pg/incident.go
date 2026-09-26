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
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	"github.com/gsoultan/storm/runtime"
)

type incidentRepository struct{ conn }

// NewIncidentRepository returns the account of why work failed.
func NewIncidentRepository(c *db.Conn) contracts.IncidentRepository {
	return &incidentRepository{conn{conn: c}}
}

// Create records an incident.
//
// Written by the worker whose job failed, so it is not scoped: an incident that
// could be refused would mean a failure with nothing recording it, which is the
// state incidents exist to end.
func (r *incidentRepository) Create(ctx context.Context, in models.IncidentModel) (models.IncidentModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.IncidentModel{}, err
	}
	ins := incident.Create()
	if id := uuid.UUID(in.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetInstanceID(uuid.UUID(in.InstanceID))
	ins.SetNodeID(in.NodeID)
	ins.SetError(in.Error)
	ins.SetStatus(string(in.Status))
	// An incident outlives the job it came from and the version it ran on, so
	// both references are optional and both are set to null rather than
	// cascading when their target goes.
	if jobID := uuid.UUID(in.JobID); jobID != uuid.Nil {
		ins.SetJobID(jobID)
	}
	if definitionID := uuid.UUID(in.DefinitionID); definitionID != uuid.Nil {
		ins.SetDefinitionID(definitionID)
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return models.IncidentModel{}, fmt.Errorf("could not record the incident: %w", err)
	}
	return incidentFrom(row), nil
}

func (r *incidentRepository) Get(ctx context.Context, id uuid.UUID) (models.IncidentModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.IncidentModel{}, err
	}
	row, found, err := incident.New().Where(incident.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return models.IncidentModel{}, fmt.Errorf("could not read the incident: %w", err)
	}
	if !found {
		return models.IncidentModel{}, fmt.Errorf("%w: no such incident", apierr.ErrNotFound)
	}
	// Scoped through the instance: an incident carries no project of its own.
	if err := r.requireInstanceInTenant(ctx, uuid.UUID(row.InstanceID)); err != nil {
		return models.IncidentModel{}, fmt.Errorf("%w: no such incident", apierr.ErrNotFound)
	}
	return incidentFrom(row), nil
}

func (r *incidentRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.IncidentModel, error) {
	if err := r.requireInstanceInTenant(ctx, instanceID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := incident.New().
		Where(incident.InstanceID.Eq(instanceID)).
		Order(incident.CreatedAt.Desc()).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the incidents: %w", err)
	}
	out := make([]models.IncidentModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, incidentFrom(row))
	}
	return out, nil
}

// CountOpen counts the incidents nobody has resolved, for the metrics endpoint.
func (r *incidentRepository) CountOpen(ctx context.Context) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: the open incidents span every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	open, err := incident.New().Where(incident.Status.Eq(string(models.IncidentOpen))).Count(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not count the open incidents: %w", err)
	}
	return open, nil
}

// Update saves a change to an incident — resolving it, mostly.
func (r *incidentRepository) Update(ctx context.Context, in models.IncidentModel) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := incident.New().Where(incident.ID.Eq(uuid.UUID(in.ID))).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the incident: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such incident", apierr.ErrNotFound)
	}
	if err := r.requireInstanceInTenant(ctx, uuid.UUID(row.InstanceID)); err != nil {
		return err
	}
	mut := incident.Mutate(row)
	mut.SetStatus(string(in.Status))
	mut.SetError(in.Error)
	if in.ResolvedAt != nil {
		mut.SetResolvedAt(*in.ResolvedAt)
	} else {
		mut.SetResolvedAtNull()
	}
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the incident: %w", err)
	}
	return nil
}

func (r *incidentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := incident.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such incident", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the incident: %w", err)
	}
	return nil
}

func incidentFrom(row incident.Row) models.IncidentModel {
	in := models.IncidentModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		InstanceID: models.UUID(row.InstanceID),
		NodeID:     row.NodeID,
		Error:      row.Error,
		Status:     models.IncidentStatus(row.Status),
	}
	if jobID, ok := row.JobID.Get(); ok {
		in.JobID = models.UUID(jobID)
	}
	if definitionID, ok := row.DefinitionID.Get(); ok {
		in.DefinitionID = models.UUID(definitionID)
	}
	if resolved, ok := row.ResolvedAt.Get(); ok {
		in.ResolvedAt = &resolved
	}
	return in
}
