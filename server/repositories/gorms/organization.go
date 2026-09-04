package gorms

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"

	"gorm.io/gorm"
)

type gormOrganizationRepository struct {
	db *gorm.DB
}

// NewOrganizationRepository creates a new GORM-based OrganizationRepository.
func NewOrganizationRepository(db *gorm.DB) contracts.OrganizationRepository {
	return &gormOrganizationRepository{db: db}
}

func (r *gormOrganizationRepository) Get(ctx context.Context, id uuid.UUID) (models.OrganizationModel, error) {
	var m models.OrganizationModel
	if err := GetTx(ctx, r.db).First(&m, QueryByID, id).Error; err != nil {
		return models.OrganizationModel{}, lookupError(err, "organization")
	}
	return m, nil
}

func (r *gormOrganizationRepository) List(ctx context.Context) ([]models.OrganizationModel, error) {
	var modelsList []models.OrganizationModel
	if err := GetTx(ctx, r.db).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list organizations: %w", err)
	}
	return modelsList, nil
}

func (r *gormOrganizationRepository) Create(ctx context.Context, m models.OrganizationModel) error {
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return fmt.Errorf("could not create organization: %w", err)
	}
	return nil
}

func (r *gormOrganizationRepository) Update(ctx context.Context, m models.OrganizationModel) error {
	result := GetTx(ctx, r.db).Save(&m)
	if result.Error != nil {
		return fmt.Errorf("could not update organization: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("organization not found: %s", m.ID)
	}
	return nil
}

func (r *gormOrganizationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := GetTx(ctx, r.db).Delete(&models.OrganizationModel{}, QueryByID, id)
	if result.Error != nil {
		return fmt.Errorf("could not delete organization: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("organization not found: %s", id)
	}
	return nil
}
