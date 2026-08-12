package gorms

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gormProcessRepository struct {
	db *gorm.DB
}

// NewProcessRepository creates a new GORM-based ProcessRepository.
func NewProcessRepository(db *gorm.DB) contracts.ProcessRepository {
	return &gormProcessRepository{db: db}
}

func (r *gormProcessRepository) Create(ctx context.Context, m models.ProcessInstanceModel) (uuid.UUID, error) {
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return uuid.Nil, fmt.Errorf("could not create process instance: %w", err)
	}
	return m.ID, nil
}

func (r *gormProcessRepository) Get(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	var m models.ProcessInstanceModel
	if err := GetTx(ctx, r.db).First(&m, QueryByID, id).Error; err != nil {
		return models.ProcessInstanceModel{}, fmt.Errorf("could not get process instance: %w", err)
	}
	return m, nil
}

func (r *gormProcessRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	var m models.ProcessInstanceModel
	if err := GetTx(ctx, r.db).Clauses(clause.Locking{Strength: "UPDATE"}).First(&m, QueryByID, id).Error; err != nil {
		return models.ProcessInstanceModel{}, fmt.Errorf("could not get process instance for update: %w", err)
	}
	return m, nil
}

func (r *gormProcessRepository) Update(ctx context.Context, m models.ProcessInstanceModel) error {
	if err := GetTx(ctx, r.db).Save(&m).Error; err != nil {
		return fmt.Errorf("could not update process instance: %w", err)
	}
	return nil
}

func (r *gormProcessRepository) List(ctx context.Context) ([]models.ProcessInstanceModel, error) {
	var modelsList []models.ProcessInstanceModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_instances")
	if err := db.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list process instances: %w", err)
	}
	return modelsList, nil
}

func (r *gormProcessRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ProcessInstanceModel, error) {
	var modelsList []models.ProcessInstanceModel
	if err := GetTx(ctx, r.db).Where(QueryByProjectID, projectID).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list process instances by project: %w", err)
	}
	return modelsList, nil
}

func (r *gormProcessRepository) ListByDefinition(ctx context.Context, definitionID uuid.UUID) ([]models.ProcessInstanceModel, error) {
	var modelsList []models.ProcessInstanceModel
	if err := GetTx(ctx, r.db).Where(QueryByDefinitionID, definitionID).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list process instances by definition: %w", err)
	}
	return modelsList, nil
}

func (r *gormProcessRepository) ListByParent(ctx context.Context, parentInstanceID uuid.UUID) ([]models.ProcessInstanceModel, error) {
	var modelsList []models.ProcessInstanceModel
	if err := GetTx(ctx, r.db).Where("parent_instance_id = ?", parentInstanceID).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list instances by parent: %w", err)
	}
	return modelsList, nil
}

func (r *gormProcessRepository) CountByStatus(ctx context.Context, projectID uuid.UUID, status models.ProcessStatus) (int64, error) {
	var count int64
	query := GetTx(ctx, r.db).Model(&models.ProcessInstanceModel{}).Where("status = ?", string(status))
	if projectID != uuid.Nil {
		query = query.Where(QueryByProjectID, projectID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("could not count instances: %w", err)
	}
	return count, nil
}

// ListByProjectPaged returns one page of a project's process instances.
func (r *gormProcessRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p contracts.Pagination) (contracts.Page[models.ProcessInstanceModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_instances").
		Model(&models.ProcessInstanceModel{}).
		Where(QueryByProjectID, projectID)
	return countAndPage[models.ProcessInstanceModel](base, p, "created_at DESC")
}

// ListPaged returns one page of process instances across the active tenant.
func (r *gormProcessRepository) ListPaged(ctx context.Context, p contracts.Pagination) (contracts.Page[models.ProcessInstanceModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_instances").
		Model(&models.ProcessInstanceModel{})
	return countAndPage[models.ProcessInstanceModel](base, p, "created_at DESC")
}
