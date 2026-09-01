package gorms

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

type variableSnapshotRepository struct {
	db *gorm.DB
}

// NewVariableSnapshotRepository creates a new GORM-backed VariableSnapshotRepository.
func NewVariableSnapshotRepository(db *gorm.DB) contracts.VariableSnapshotRepository {
	return &variableSnapshotRepository{db: db}
}

func (r *variableSnapshotRepository) Create(ctx context.Context, m models.VariableSnapshotModel) (models.VariableSnapshotModel, error) {
	err := GetTx(ctx, r.db).Create(&m).Error
	return m, err
}

func (r *variableSnapshotRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.VariableSnapshotModel, error) {
	var ms []models.VariableSnapshotModel
	err := GetTx(ctx, r.db).
		Where("instance_id = ?", instanceID).
		Order("captured_at ASC").
		Find(&ms).Error
	if err != nil {
		return nil, err
	}
	return ms, nil
}
