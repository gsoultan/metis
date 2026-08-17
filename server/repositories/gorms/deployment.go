package gorms

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

type gormDeploymentRepository struct {
	db *gorm.DB
}

func NewDeploymentRepository(db *gorm.DB) contracts.DeploymentRepository {
	return &gormDeploymentRepository{db: db}
}

func (r *gormDeploymentRepository) Create(ctx context.Context, d models.DeploymentModel) error {
	// Refuse a deployment planted in another organization's project.
	if err := requireProjectInTenant(ctx, GetTx(ctx, r.db), uuid.UUID(d.ProjectID)); err != nil {
		return err
	}
	return GetTx(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		return nil
	})
}

// tableDeployments and tableDeploymentResources are the SQL tables behind
// DeploymentModel and ResourceModel, needed by name so the tenant scope can
// build its JOINs.
const (
	tableDeployments         = "deployments"
	tableDeploymentResources = "deployment_resources"
)

// Get returns a deployment by ID, scoped to the caller's tenant — an ID from
// another organization reads as not found.
func (r *gormDeploymentRepository) Get(ctx context.Context, id uuid.UUID) (models.DeploymentModel, error) {
	var m models.DeploymentModel
	if err := tenantScopeDB(ctx, GetTx(ctx, r.db), tableDeployments).
		Preload("Resources").
		First(&m, QualifiedByID(tableDeployments), id).Error; err != nil {
		return models.DeploymentModel{}, err
	}
	return m, nil
}

// ListByProject lists a project's deployments, scoped to the caller's tenant.
func (r *gormDeploymentRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.DeploymentModel, error) {
	var modelsList []models.DeploymentModel
	query := tenantScopeDB(ctx, GetTx(ctx, r.db), tableDeployments)
	if projectID != uuid.Nil {
		query = query.Where("deployments.project_id = ?", projectID)
	}
	if err := query.Find(&modelsList).Error; err != nil {
		return nil, err
	}
	return modelsList, nil
}

// GetResource returns a deployed resource by ID. Resources carry no project of
// their own, so the scope reaches the tenant through the parent deployment.
func (r *gormDeploymentRepository) GetResource(ctx context.Context, id uuid.UUID) (models.ResourceModel, error) {
	var m models.ResourceModel
	if err := tenantScopeDeploymentResources(ctx, GetTx(ctx, r.db)).
		First(&m, QualifiedByID(tableDeploymentResources), id).Error; err != nil {
		return models.ResourceModel{}, err
	}
	return m, nil
}

// ListResources lists a deployment's resources, scoped through that deployment
// to the caller's tenant.
func (r *gormDeploymentRepository) ListResources(ctx context.Context, deploymentID uuid.UUID) ([]models.ResourceModel, error) {
	var modelsList []models.ResourceModel
	if err := tenantScopeDeploymentResources(ctx, GetTx(ctx, r.db)).
		Find(&modelsList, "deployment_resources.deployment_id = ?", deploymentID).Error; err != nil {
		return nil, err
	}
	return modelsList, nil
}
