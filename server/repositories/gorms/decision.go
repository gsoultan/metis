package gorms

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

	"gorm.io/gorm"
)

type gormDecisionRepository struct {
	db *gorm.DB
}

// NewDecisionRepository creates a new GORM-based DecisionRepository.
func NewDecisionRepository(db *gorm.DB) contracts.DecisionRepository {
	return &gormDecisionRepository{db: db}
}

func (r *gormDecisionRepository) Get(ctx context.Context, id uuid.UUID) (models.DecisionDefinitionModel, error) {
	var m models.DecisionDefinitionModel
	if err := GetTx(ctx, r.db).First(&m, QueryByID, id).Error; err != nil {
		return models.DecisionDefinitionModel{}, fmt.Errorf("could not get decision: %w", err)
	}
	return m, nil
}

func (r *gormDecisionRepository) GetByKey(ctx context.Context, key string) (models.DecisionDefinitionModel, error) {
	var m models.DecisionDefinitionModel
	// GetConnector latest version
	if err := GetTx(ctx, r.db).Order(OrderLatestDefinition).Where(ByKey(key)).First(&m).Error; err != nil {
		return models.DecisionDefinitionModel{}, fmt.Errorf("could not get decision by key: %w", err)
	}
	return m, nil
}
func (r *gormDecisionRepository) GetByKeyAndVersion(ctx context.Context, key string, version int) (models.DecisionDefinitionModel, error) {
	var m models.DecisionDefinitionModel
	if err := GetTx(ctx, r.db).Where(ByKeyAndVersion(key, version)).First(&m).Error; err != nil {
		return models.DecisionDefinitionModel{}, fmt.Errorf("could not get decision by key and version: %w", err)
	}
	return m, nil
}

func (r *gormDecisionRepository) List(ctx context.Context) ([]models.DecisionDefinitionModel, error) {
	var modelsList []models.DecisionDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), "decision_definitions")
	if err := db.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list decisions: %w", err)
	}
	return modelsList, nil
}

func (r *gormDecisionRepository) Create(ctx context.Context, m models.DecisionDefinitionModel) error {
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return fmt.Errorf("could not create decision: %w", err)
	}
	return nil
}

func (r *gormDecisionRepository) Update(ctx context.Context, id uuid.UUID, m models.DecisionDefinitionModel) error {
	m.ID = models.UUID(id)
	if err := GetTx(ctx, r.db).Save(&m).Error; err != nil {
		return fmt.Errorf("could not update decision: %w", err)
	}
	return nil
}

func (r *gormDecisionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := GetTx(ctx, r.db).Delete(&models.DecisionDefinitionModel{}, QueryByID, id).Error; err != nil {
		return fmt.Errorf("could not delete decision: %w", err)
	}
	return nil
}

// ListByProjectPaged returns one page of a project's decisions, newest first.
// The order is table-qualified for the same reason definitions' is.
func (r *gormDecisionRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p contracts.Pagination) (contracts.Page[models.DecisionDefinitionModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "decision_definitions").
		Model(&models.DecisionDefinitionModel{}).
		Where("decision_definitions.project_id = ?", projectID)
	return countAndPage[models.DecisionDefinitionModel](base, p, "decision_definitions.created_at DESC")
}

func (r *gormDecisionRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.DecisionDefinitionModel, error) {
	var modelsList []models.DecisionDefinitionModel
	if err := GetTx(ctx, r.db).Where(QueryByProjectID, projectID).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list decisions by project: %w", err)
	}
	return modelsList, nil
}
