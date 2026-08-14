package impl

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
	repocont "github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// variableHistoryService implements VariableHistoryService using a
// VariableSnapshotRepository for persistence.
type variableHistoryService struct {
	repo repocont.VariableSnapshotRepository
}

// NewVariableHistoryService creates a new variableHistoryService.
func NewVariableHistoryService(repo repocont.VariableSnapshotRepository) contracts.VariableHistoryService {
	return &variableHistoryService{repo: repo}
}

// CaptureSnapshot persists a new variable snapshot for audit purposes.
func (s *variableHistoryService) CaptureSnapshot(ctx context.Context, snapshot entities.VariableSnapshot) error {
	if snapshot.ID == (uuid.UUID{}) {
		id, _ := uuid.NewV7()
		snapshot.ID = id
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now()
	}
	var instanceID uuid.UUID
	if snapshot.Instance != nil {
		instanceID = snapshot.Instance.ID
	}
	m := models.VariableSnapshotModel{
		InstanceID: models.UUID(instanceID),
		NodeID: func() string {
			if snapshot.Node != nil {
				return snapshot.Node.ID
			}
			return ""
		}(),
		Variables:  snapshot.Variables,
		CapturedAt: snapshot.CapturedAt,
	}
	m.ID = models.UUID(snapshot.ID)
	_, err := s.repo.Create(ctx, m)
	return err
}

// ListSnapshots returns all snapshots for the given instance ordered by capture time.
func (s *variableHistoryService) ListSnapshots(ctx context.Context, instanceID uuid.UUID) ([]entities.VariableSnapshot, error) {
	ms, err := s.repo.ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return toSnapshotEntities(ms), nil
}

// GetVariableHistory returns snapshots that contain the given variable name.
func (s *variableHistoryService) GetVariableHistory(ctx context.Context, instanceID uuid.UUID, variableName string) ([]entities.VariableSnapshot, error) {
	all, err := s.ListSnapshots(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	var filtered []entities.VariableSnapshot
	for _, snap := range all {
		if _, ok := snap.Variables[variableName]; ok {
			filtered = append(filtered, snap)
		}
	}
	return filtered, nil
}

// toSnapshotEntities converts a slice of models to domain entities.
func toSnapshotEntities(ms []models.VariableSnapshotModel) []entities.VariableSnapshot {
	result := make([]entities.VariableSnapshot, len(ms))
	for i, m := range ms {
		result[i] = entities.VariableSnapshot{
			ID:         uuid.UUID(m.ID),
			Instance:   &entities.ProcessInstance{ID: uuid.UUID(m.InstanceID)},
			Node:       &entities.Node{ID: m.NodeID},
			Variables:  m.Variables,
			CapturedAt: m.CapturedAt,
		}
	}
	return result
}
