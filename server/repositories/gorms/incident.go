package gorms

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

type incidentRepository struct {
	db *gorm.DB
}

func NewIncidentRepository(db *gorm.DB) contracts.IncidentRepository {
	return &incidentRepository{db: db}
}

func (r *incidentRepository) Create(ctx context.Context, m models.IncidentModel) (models.IncidentModel, error) {
	err := GetTx(ctx, r.db).Create(&m).Error
	return m, err
}

func (r *incidentRepository) Get(ctx context.Context, id uuid.UUID) (models.IncidentModel, error) {
	var m models.IncidentModel
	err := GetTx(ctx, r.db).First(&m, "id = ?", id).Error
	return m, err
}

func (r *incidentRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.IncidentModel, error) {
	var ms []models.IncidentModel
	err := GetTx(ctx, r.db).Where("instance_id = ?", instanceID).Find(&ms).Error
	if err != nil {
		return nil, err
	}
	return ms, nil
}

func (r *incidentRepository) Update(ctx context.Context, m models.IncidentModel) error {
	return GetTx(ctx, r.db).Save(&m).Error
}

func (r *incidentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return GetTx(ctx, r.db).Delete(&models.IncidentModel{}, "id = ?", id).Error
}
