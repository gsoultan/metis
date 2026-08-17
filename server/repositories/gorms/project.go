package gorms

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

	"gorm.io/gorm"
)

type gormProjectRepository struct {
	db *gorm.DB
}

// NewProjectRepository creates a new GORM-based ProjectRepository.
func NewProjectRepository(db *gorm.DB) contracts.ProjectRepository {
	return &gormProjectRepository{db: db}
}

// tableProjects is the SQL table behind ProjectModel. Projects carry
// organization_id themselves, so they scope directly rather than joining.
const tableProjects = "projects"

// Get returns a project by ID, scoped to the caller's tenant.
func (r *gormProjectRepository) Get(ctx context.Context, id uuid.UUID) (models.ProjectModel, error) {
	var m models.ProjectModel
	db := tenantScopeOrganization(ctx, GetTx(ctx, r.db), tableProjects)
	if err := db.First(&m, QualifiedByID(tableProjects), id).Error; err != nil {
		return models.ProjectModel{}, fmt.Errorf("could not get project: %w", err)
	}
	return m, nil
}

// List returns the caller's projects. Unscoped, this returned every project on
// the installation — the whole customer list, to anyone authenticated.
func (r *gormProjectRepository) List(ctx context.Context) ([]models.ProjectModel, error) {
	var modelsList []models.ProjectModel
	db := tenantScopeOrganization(ctx, GetTx(ctx, r.db), tableProjects)
	if err := db.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list projects: %w", err)
	}
	return modelsList, nil
}

// ListByOrganization filters by an organization the caller names, still bounded
// by the one they are actually in — naming someone else's returns nothing.
func (r *gormProjectRepository) ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.ProjectModel, error) {
	var modelsList []models.ProjectModel
	query := tenantScopeOrganization(ctx, GetTx(ctx, r.db), tableProjects)
	if organizationID != uuid.Nil {
		query = query.Where("projects.organization_id = ?", organizationID)
	}
	if err := query.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list projects: %w", err)
	}
	return modelsList, nil
}

// Create refuses a project planted in another organization.
func (r *gormProjectRepository) Create(ctx context.Context, p models.ProjectModel) error {
	if err := requireOwnOrganization(ctx, uuid.UUID(p.OrganizationID)); err != nil {
		return err
	}
	if err := GetTx(ctx, r.db).Create(&p).Error; err != nil {
		return fmt.Errorf("could not create project: %w", err)
	}
	return nil
}

// Update saves a project, refusing an ID outside the caller's tenant.
func (r *gormProjectRepository) Update(ctx context.Context, p models.ProjectModel) error {
	db := GetTx(ctx, r.db)
	if err := requireProjectInTenant(ctx, db, uuid.UUID(p.ID)); err != nil {
		return err
	}
	// Re-assigning a project to another organization would move it, and
	// everything under it, out of reach in one write.
	if err := requireOwnOrganization(ctx, uuid.UUID(p.OrganizationID)); err != nil {
		return err
	}
	result := db.Save(&p)
	if result.Error != nil {
		return fmt.Errorf("could not update project: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("project not found: %s", p.ID)
	}
	return nil
}

// Delete removes a project, refusing an ID outside the caller's tenant.
func (r *gormProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := GetTx(ctx, r.db)
	if err := requireProjectInTenant(ctx, db, id); err != nil {
		return err
	}
	result := db.Delete(&models.ProjectModel{}, QualifiedByID(tableProjects), id)
	if result.Error != nil {
		return fmt.Errorf("could not delete project: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("project not found: %s", id)
	}
	return nil
}
