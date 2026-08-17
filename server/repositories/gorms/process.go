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
	// Refuse a process instance planted in another organization's project.
	if err := requireProjectInTenant(ctx, GetTx(ctx, r.db), uuid.UUID(m.ProjectID)); err != nil {
		return uuid.Nil, err
	}
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return uuid.Nil, fmt.Errorf("could not create process instance: %w", err)
	}
	return uuid.UUID(m.ID), nil
}

// tableProcessInstances is the SQL table behind ProcessInstanceModel, needed by
// name so the tenant scope can build its clauses.
const tableProcessInstances = "process_instances"

// Get returns a process instance by ID, scoped to the caller's tenant.
func (r *gormProcessRepository) Get(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	var m models.ProcessInstanceModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableProcessInstances)
	if err := db.First(&m, QualifiedByID(tableProcessInstances), id).Error; err != nil {
		return models.ProcessInstanceModel{}, fmt.Errorf("could not get process instance: %w", err)
	}
	return m, nil
}

// GetForUpdate is Get with a row lock. The scope is the subquery form here: this
// statement takes FOR UPDATE, and joining projects would lock rows in that table
// too on every instance the engine touches.
func (r *gormProcessRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	var m models.ProcessInstanceModel
	db := tenantScopeCondition(ctx, GetTx(ctx, r.db), tableProcessInstances)
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&m, QualifiedByID(tableProcessInstances), id).Error; err != nil {
		return models.ProcessInstanceModel{}, fmt.Errorf("could not get process instance for update: %w", err)
	}
	return m, nil
}

// Update saves instance state, refusing an ID outside the caller's tenant.
func (r *gormProcessRepository) Update(ctx context.Context, m models.ProcessInstanceModel) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableProcessInstances, &models.ProcessInstanceModel{}, uuid.UUID(m.ID)); err != nil {
		return err
	}
	if err := db.Save(&m).Error; err != nil {
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
	return countAndPage[models.ProcessInstanceModel](base, p, "process_instances.created_at DESC")
}

// ListPaged returns one page of process instances across the active tenant.
func (r *gormProcessRepository) ListPaged(ctx context.Context, p contracts.Pagination) (contracts.Page[models.ProcessInstanceModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_instances").
		Model(&models.ProcessInstanceModel{})
	return countAndPage[models.ProcessInstanceModel](base, p, "process_instances.created_at DESC")
}
