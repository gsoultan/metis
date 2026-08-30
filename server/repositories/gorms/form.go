package gorms

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

type gormFormRepository struct {
	db *gorm.DB
}

func NewFormRepository(db *gorm.DB) contracts.FormRepository {
	return &gormFormRepository{db: db}
}

func (r *gormFormRepository) Create(ctx context.Context, f models.FormModel) error {
	// Refuse a form planted in another organization's project.
	if err := requireProjectInTenant(ctx, GetTx(ctx, r.db), uuid.UUID(f.ProjectID)); err != nil {
		return err
	}
	return GetTx(ctx, r.db).Create(&f).Error
}

// tableForms is the SQL table behind FormModel, needed by name so the tenant
// scope can build its JOIN.
const tableForms = "forms"

// Get returns a form by ID, scoped to the caller's tenant — an ID belonging to
// another organization reads as not found rather than as a form.
func (r *gormFormRepository) Get(ctx context.Context, id uuid.UUID) (models.FormModel, error) {
	var m models.FormModel
	if err := tenantScopeDB(ctx, GetTx(ctx, r.db), tableForms).
		First(&m, QualifiedByID(tableForms), id).Error; err != nil {
		return models.FormModel{}, err
	}
	return m, nil
}

// GetByKey returns a project's form by key, scoped to the caller's tenant.
//
// `key` is quoted through a map because it is a reserved word in MySQL; the
// project condition stays a raw clause so it can be table-qualified against the
// projects join.
func (r *gormFormRepository) GetByKey(ctx context.Context, projectID uuid.UUID, key string) (models.FormModel, error) {
	var m models.FormModel
	if err := tenantScopeDB(ctx, GetTx(ctx, r.db), tableForms).
		Where("forms.project_id = ?", projectID).
		Where(ByKey(key)).
		First(&m).Error; err != nil {
		return models.FormModel{}, err
	}
	return m, nil
}

// ListByProject lists a project's forms, scoped to the caller's tenant. A nil
// project ID means "every form I may see", which the scope bounds to the
// caller's organization.
func (r *gormFormRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.FormModel, error) {
	var modelsList []models.FormModel
	query := tenantScopeDB(ctx, GetTx(ctx, r.db), tableForms)
	if projectID != uuid.Nil {
		query = query.Where("forms.project_id = ?", projectID)
	}
	if err := query.Find(&modelsList).Error; err != nil {
		return nil, err
	}
	return modelsList, nil
}

// Delete removes a form, refusing an ID outside the caller's tenant.
func (r *gormFormRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableForms, &models.FormModel{}, id); err != nil {
		return err
	}
	return db.Delete(&models.FormModel{}, QualifiedByID(tableForms), id).Error
}
