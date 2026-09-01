package gorms

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

type compensatableActivityRepository struct {
	db *gorm.DB
}

// NewCompensatableActivityRepository creates a new GORM-backed CompensatableActivityRepository.
func NewCompensatableActivityRepository(db *gorm.DB) contracts.CompensatableActivityRepository {
	return &compensatableActivityRepository{db: db}
}

func (r *compensatableActivityRepository) Create(ctx context.Context, m models.CompensatableActivityModel) (models.CompensatableActivityModel, error) {
	err := GetTx(ctx, r.db).Create(&m).Error
	return m, err
}

func (r *compensatableActivityRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.CompensatableActivityModel, error) {
	var ms []models.CompensatableActivityModel
	err := GetTx(ctx, r.db).
		Where("instance_id = ? AND compensated = ?", instanceID, false).
		Order("completed_at DESC").
		Find(&ms).Error
	if err != nil {
		return nil, err
	}
	return ms, nil
}

func (r *compensatableActivityRepository) MarkCompensated(ctx context.Context, id uuid.UUID) error {
	return GetTx(ctx, r.db).
		Model(&models.CompensatableActivityModel{}).
		Where("id = ?", id).
		Update("compensated", true).Error
}
