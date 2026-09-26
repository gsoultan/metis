package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/servicecall"
	"github.com/gsoultan/storm/runtime"
)

type serviceCallRepository struct{ conn }

// NewServiceCallRepository returns the record of outbound calls.
func NewServiceCallRepository(c *db.Conn) contracts.ServiceCallRepository {
	return &serviceCallRepository{conn{conn: c}}
}

// Begin records that a call is about to be made, or returns the record of one
// that already was.
//
// The unique key on (instance, node, iteration) is what makes a retry find the
// call rather than make a second one, and it is declared across the deleted
// rows so a marked row keeps holding it. The insert races deliberately: two
// workers reaching the same step both try, one wins on the constraint, and the
// loser reads back what the winner wrote. Checking first and inserting second
// would leave a window where both check, both find nothing and both call.
//
// Unscoped. This runs inside the engine on behalf of a process that is already
// running, and a refusal would mean an outbound call with no record of it.
func (r *serviceCallRepository) Begin(ctx context.Context, call models.ServiceCallModel) (models.ServiceCallModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ServiceCallModel{}, err
	}
	id := uuid.UUID(call.ID)
	if id == uuid.Nil {
		if id, err = uuid.NewV7(); err != nil {
			return models.ServiceCallModel{}, fmt.Errorf("could not generate a service call id: %w", err)
		}
	}

	ins := servicecall.Create()
	ins.SetID(id)
	ins.SetInstanceID(uuid.UUID(call.InstanceID))
	ins.SetProjectID(uuid.UUID(call.ProjectID))
	ins.SetNodeID(call.NodeID)
	ins.SetIterationID(call.IterationID)
	ins.SetIdempotencyKey(call.IdempotencyKey)
	ins.SetStatus(models.ServiceCallInFlight)
	ins.SetAttempts(1)
	row, err := ins.Insert(ctx, ex)
	if err == nil {
		return serviceCallFrom(row)
	}
	if !errors.Is(err, runtime.ErrUniqueViolation) {
		return models.ServiceCallModel{}, fmt.Errorf("could not record the service call: %w", err)
	}

	existing, err := r.Get(ctx, uuid.UUID(call.InstanceID), call.NodeID, call.IterationID)
	if err != nil {
		return models.ServiceCallModel{}, err
	}
	// Count the attempt, so an operator reading this table during an incident
	// can see that the call was started more than once.
	if existing.Status == models.ServiceCallInFlight {
		stored, found, err := servicecall.New().Where(servicecall.ID.Eq(uuid.UUID(existing.ID))).One(ctx, ex)
		if err != nil {
			return models.ServiceCallModel{}, fmt.Errorf("could not read the service call: %w", err)
		}
		if found {
			mut := servicecall.Mutate(stored)
			mut.SetAttempts(stored.Attempts + 1)
			if err := mut.Update(ctx, ex); err != nil {
				return models.ServiceCallModel{}, fmt.Errorf("could not count the service call attempt: %w", err)
			}
			existing.Attempts++
		}
	}
	return existing, nil
}

// Complete records what the call returned.
func (r *serviceCallRepository) Complete(ctx context.Context, id uuid.UUID, response map[string]any) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	encoded, err := sealedJSONOf(response)
	if err != nil {
		return fmt.Errorf("could not encode the service call's response: %w", err)
	}
	row, found, err := servicecall.New().Where(servicecall.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the service call: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such service call", apierr.ErrNotFound)
	}
	mut := servicecall.Mutate(row)
	mut.SetStatus(models.ServiceCallCompleted)
	mut.SetResponse(encoded)
	mut.SetCompletedAt(time.Now().UTC())
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not complete the service call: %w", err)
	}
	return nil
}

// Get returns the record for one step of one instance.
func (r *serviceCallRepository) Get(ctx context.Context, instanceID uuid.UUID, nodeID, iterationID string) (models.ServiceCallModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ServiceCallModel{}, err
	}
	row, found, err := servicecall.New().
		Where(
			servicecall.InstanceID.Eq(instanceID),
			servicecall.NodeID.Eq(nodeID),
			servicecall.IterationID.Eq(iterationID),
		).
		One(ctx, ex)
	if err != nil {
		return models.ServiceCallModel{}, fmt.Errorf("could not read the service call: %w", err)
	}
	if !found {
		return models.ServiceCallModel{}, fmt.Errorf("%w: no such service call", apierr.ErrNotFound)
	}
	return serviceCallFrom(row)
}

func serviceCallFrom(row servicecall.Row) (models.ServiceCallModel, error) {
	response, err := sealedMapOf(row.Response)
	if err != nil {
		return models.ServiceCallModel{}, fmt.Errorf("could not decode a service call's response: %w", err)
	}
	call := models.ServiceCallModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		InstanceID:     models.UUID(row.InstanceID),
		ProjectID:      models.UUID(row.ProjectID),
		NodeID:         row.NodeID,
		IterationID:    row.IterationID,
		IdempotencyKey: row.IdempotencyKey,
		Status:         row.Status,
		Attempts:       int(row.Attempts),
		Response:       response,
	}
	if completed, ok := row.CompletedAt.Get(); ok {
		call.CompletedAt = &completed
	}
	return call, nil
}
