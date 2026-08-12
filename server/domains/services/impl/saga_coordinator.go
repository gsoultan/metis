package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	contracts2 "github.com/gsoultan/gobpm/server/domains/services/contracts"
	repocont "github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// sagaCoordinator implements SagaCoordinator using the Repository pattern for
// persistence and the ExecutionEngine to trigger compensation nodes.
type sagaCoordinator struct {
	repo   repocont.CompensatableActivityRepository
	engine contracts2.ExecutionEngine
}

// NewSagaCoordinator creates a new sagaCoordinator.
func NewSagaCoordinator(repo repocont.CompensatableActivityRepository, engine contracts2.ExecutionEngine) contracts2.SagaCoordinator {
	return &sagaCoordinator{repo: repo, engine: engine}
}

// RecordActivity persists a completed activity so it can be compensated later.
func (s *sagaCoordinator) RecordActivity(ctx context.Context, activity entities.CompensatableActivity) error {
	if activity.ID == (uuid.UUID{}) {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("saga: generate activity ID: %w", err)
		}
		activity.ID = id
	}
	if activity.CompletedAt.IsZero() {
		activity.CompletedAt = time.Now()
	}
	var instanceID uuid.UUID
	if activity.Instance != nil {
		instanceID = activity.Instance.ID
	}
	var nodeID, compNodeID string
	if activity.Node != nil {
		nodeID = activity.Node.ID
	}
	if activity.CompensationNode != nil {
		compNodeID = activity.CompensationNode.ID
	}
	m := models.CompensatableActivityModel{
		InstanceID:         instanceID,
		NodeID:             nodeID,
		CompensationNodeID: compNodeID,
		Variables:          activity.Variables,
		CompletedAt:        activity.CompletedAt,
	}
	m.ID = activity.ID
	_, err := s.repo.Create(ctx, m)
	return err
}

// Compensate retrieves all uncompensated activities for the instance in reverse
// completion order and executes each compensation node via the engine.
func (s *sagaCoordinator) Compensate(ctx context.Context, instanceID uuid.UUID) error {
	activities, err := s.repo.ListByInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("saga: list activities for instance %s: %w", instanceID, err)
	}
	instance, err := s.engine.GetInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("saga: get instance %s: %w", instanceID, err)
	}
	def, err := s.engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return fmt.Errorf("saga: get definition for instance %s: %w", instanceID, err)
	}
	for _, m := range activities {
		if err := s.compensateActivity(ctx, m, &instance, def); err != nil {
			return err
		}
	}
	return nil
}

// compensateActivity executes a single compensation node and marks the activity as compensated.
func (s *sagaCoordinator) compensateActivity(ctx context.Context, m models.CompensatableActivityModel, instance *entities.ProcessInstance, def *entities.ProcessDefinition) error {
	activity := adapters.CompensatableActivityEntityAdapter{Model: m}.ToEntity()
	if activity.CompensationNode == nil || activity.CompensationNode.ID == "" {
		return s.repo.MarkCompensated(ctx, activity.ID)
	}
	// Restore variable snapshot for this activity into the instance.
	for k, v := range activity.Variables {
		instance.SetVariable(k, v)
	}
	if err := s.engine.ExecuteNode(ctx, instance, def, activity.CompensationNode.ID); err != nil {
		return fmt.Errorf("saga: execute compensation node %s: %w", activity.CompensationNode.ID, err)
	}
	return s.repo.MarkCompensated(ctx, activity.ID)
}
