package gorms

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

	"gorm.io/gorm"
)

type gormDefinitionRepository struct {
	db *gorm.DB
}

// NewDefinitionRepository creates a new GORM-based DefinitionRepository.
func NewDefinitionRepository(db *gorm.DB) contracts.DefinitionRepository {
	return &gormDefinitionRepository{db: db}
}

func (r *gormDefinitionRepository) Get(ctx context.Context, id uuid.UUID) (models.ProcessDefinitionModel, error) {
	var m models.ProcessDefinitionModel
	if err := GetTx(ctx, r.db).First(&m, QueryByID, id).Error; err != nil {
		return models.ProcessDefinitionModel{}, fmt.Errorf("could not get definition: %w", err)
	}
	return m, nil
}

func (r *gormDefinitionRepository) GetByKey(ctx context.Context, key string) (models.ProcessDefinitionModel, error) {
	var m models.ProcessDefinitionModel
	// GetConnector latest version
	if err := GetTx(ctx, r.db).Order(OrderLatestDefinition).First(&m, SelectLatestDefinition, key).Error; err != nil {
		return models.ProcessDefinitionModel{}, fmt.Errorf("could not get definition by key: %w", err)
	}
	return m, nil
}
func (r *gormDefinitionRepository) GetByKeyAndVersion(ctx context.Context, key string, version int) (models.ProcessDefinitionModel, error) {
	var m models.ProcessDefinitionModel
	if err := GetTx(ctx, r.db).Where("key = ? AND version = ?", key, version).First(&m).Error; err != nil {
		return models.ProcessDefinitionModel{}, fmt.Errorf("could not get definition by key and version: %w", err)
	}
	return m, nil
}

func (r *gormDefinitionRepository) List(ctx context.Context) ([]models.ProcessDefinitionModel, error) {
	var modelsList []models.ProcessDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_definitions")
	if err := db.Select("process_definitions.id", "process_definitions.project_id", "process_definitions.key", "process_definitions.name", "process_definitions.version", "process_definitions.created_at").Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list definitions: %w", err)
	}
	return modelsList, nil
}

func (r *gormDefinitionRepository) Create(ctx context.Context, m models.ProcessDefinitionModel) error {
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return fmt.Errorf("could not create definition: %w", err)
	}
	return nil
}

func (r *gormDefinitionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := GetTx(ctx, r.db).Delete(&models.ProcessDefinitionModel{}, QueryByID, id).Error; err != nil {
		return fmt.Errorf("could not delete definition: %w", err)
	}
	return nil
}

func (r *gormDefinitionRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ProcessDefinitionModel, error) {
	var modelsList []models.ProcessDefinitionModel
	if err := GetTx(ctx, r.db).Select("id", "project_id", "key", "name", "version", "created_at").Where(QueryByProjectID, projectID).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list definitions by project: %w", err)
	}
	return modelsList, nil
}
