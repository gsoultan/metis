package gorms

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

// tableServiceCalls is the SQL table behind ServiceCallModel.
const tableServiceCalls = "service_calls"

type gormServiceCallRepository struct {
	db *gorm.DB
}

// NewServiceCallRepository creates a new GORM-based ServiceCallRepository.
func NewServiceCallRepository(db *gorm.DB) contracts.ServiceCallRepository {
	return &gormServiceCallRepository{db: db}
}

// Begin claims a call, or reports what a previous attempt already did.
//
// The insert is the claim, and the unique index on
// (instance_id, node_id, iteration_id) is what makes it one. Two job workers can
// pick up the same job — that is what a lock expiry means — and a check-then-
// insert would let both conclude they were first and both call the downstream.
// Losing the insert is therefore not an error: it is the answer.
func (r *gormServiceCallRepository) Begin(ctx context.Context, call models.ServiceCallModel) (models.ServiceCallModel, error) {
	if call.ID == models.NilUUID {
		id, err := uuid.NewV7()
		if err != nil {
			return models.ServiceCallModel{}, fmt.Errorf("could not generate a service call id: %w", err)
		}
		call.ID = models.UUID(id)
	}
	call.Status = models.ServiceCallInFlight
	call.Attempts = 1

	err := GetTx(ctx, r.db).Create(&call).Error
	if err == nil {
		return call, nil
	}
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		return models.ServiceCallModel{}, fmt.Errorf("could not record the service call: %w", err)
	}

	existing, err := r.Get(ctx, uuid.UUID(call.InstanceID), call.NodeID, call.IterationID)
	if err != nil {
		return models.ServiceCallModel{}, err
	}
	// Count the attempt, so an operator reading this table during an incident
	// can see that the call was started more than once.
	if existing.Status == models.ServiceCallInFlight {
		if err := GetTx(ctx, r.db).Model(&models.ServiceCallModel{}).
			Where(QualifiedByID(tableServiceCalls), uuid.UUID(existing.ID)).
			UpdateColumn("attempts", gorm.Expr("attempts + 1")).Error; err != nil {
			return models.ServiceCallModel{}, fmt.Errorf("could not count the service call attempt: %w", err)
		}
		existing.Attempts++
	}
	return existing, nil
}

func (r *gormServiceCallRepository) Complete(ctx context.Context, id uuid.UUID, response map[string]any) error {
	now := time.Now()
	// The struct form so the response goes through the model's JSON serializer
	// rather than being handed to the driver as a Go map.
	err := GetTx(ctx, r.db).Model(&models.ServiceCallModel{}).
		Where(QualifiedByID(tableServiceCalls), id).
		Select("status", "response", "completed_at").
		Updates(models.ServiceCallModel{
			Status:      models.ServiceCallCompleted,
			Response:    response,
			CompletedAt: &now,
		}).Error
	if err != nil {
		return fmt.Errorf("could not complete the service call: %w", err)
	}
	return nil
}

func (r *gormServiceCallRepository) Get(ctx context.Context, instanceID uuid.UUID, nodeID, iterationID string) (models.ServiceCallModel, error) {
	var m models.ServiceCallModel
	err := GetTx(ctx, r.db).
		Where("instance_id = ? AND node_id = ? AND iteration_id = ?", instanceID, nodeID, iterationID).
		First(&m).Error
	if err != nil {
		return models.ServiceCallModel{}, fmt.Errorf("could not read the service call: %w", err)
	}
	return m, nil
}
