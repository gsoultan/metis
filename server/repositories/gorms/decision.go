package gorms

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"

	"gorm.io/gorm"
)

type gormDecisionRepository struct {
	db *gorm.DB
}

// NewDecisionRepository creates a new GORM-based DecisionRepository.
func NewDecisionRepository(db *gorm.DB) contracts.DecisionRepository {
	return &gormDecisionRepository{db: db}
}

// Get returns a decision by ID, scoped to the caller's tenant. The row carries
// the rule table, which is the organization's business policy in full.
func (r *gormDecisionRepository) Get(ctx context.Context, id uuid.UUID) (models.DecisionDefinitionModel, error) {
	var m models.DecisionDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableDecisionDefinitions)
	if err := db.First(&m, QualifiedByID(tableDecisionDefinitions), id).Error; err != nil {
		return models.DecisionDefinitionModel{}, lookupError(err, "decision")
	}
	return m, nil
}

// GetByKey returns the latest version of a decision, scoped to the caller's
// tenant. Keys are per project, so two organizations can hold the same one.
func (r *gormDecisionRepository) GetByKey(ctx context.Context, key string) (models.DecisionDefinitionModel, error) {
	var m models.DecisionDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableDecisionDefinitions)
	if err := db.Order(OrderLatestDefinition).Where(ByKey(key)).First(&m).Error; err != nil {
		return models.DecisionDefinitionModel{}, lookupError(err, "decision by key")
	}
	return m, nil
}

// GetByKeyAndVersion pins one version of a decision, scoped to the caller's
// tenant for the same reason GetByKey is.
func (r *gormDecisionRepository) GetByKeyAndVersion(ctx context.Context, key string, version int) (models.DecisionDefinitionModel, error) {
	var m models.DecisionDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableDecisionDefinitions)
	if err := db.Where(ByKeyAndVersion(key, version)).First(&m).Error; err != nil {
		return models.DecisionDefinitionModel{}, lookupError(err, "decision by key and version")
	}
	return m, nil
}

// NextVersion returns the version number a new deployment of key should claim.
//
// It counts over the same rows the unique index covers — one project, one key,
// soft-deleted rows included. Including them is the point of Unscoped: a version
// number that has been used once must never be reused, or the deletion of a
// decision would let its successor quietly inherit its number, and the unique
// index would refuse the write anyway.
//
// The answer is a proposal, not a reservation. Two callers asking at the same
// moment get the same number, and the unique index settles it — see
// decisionService.CreateDecision.
func (r *gormDecisionRepository) NextVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error) {
	db := tenantScopeDB(ctx,
		GetTx(ctx, r.db).Unscoped().Model(&models.DecisionDefinitionModel{}),
		tableDecisionDefinitions)

	var highest int
	if err := db.
		Where(QualifiedByProjectID(tableDecisionDefinitions), projectID).
		Where(ByKey(key)).
		Select(QueryHighestVersion(tableDecisionDefinitions)).
		Scan(&highest).Error; err != nil {
		return 0, fmt.Errorf("could not read the highest decision version: %w", err)
	}
	return highest + 1, nil
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
	// Refuse a decision planted in another organization's project.
	if err := requireProjectInTenant(ctx, GetTx(ctx, r.db), uuid.UUID(m.ProjectID)); err != nil {
		return err
	}
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return fmt.Errorf("could not create decision: %w", err)
	}
	return nil
}

// tableDecisionDefinitions is the SQL table behind DecisionDefinitionModel,
// needed by name so the tenant scope can build its clauses.
const tableDecisionDefinitions = "decision_definitions"

// Update saves a decision, refusing an ID outside the caller's tenant. A
// decision is business policy, so an unscoped write is a way to change what
// another organization's processes decide.
func (r *gormDecisionRepository) Update(ctx context.Context, id uuid.UUID, m models.DecisionDefinitionModel) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableDecisionDefinitions, &models.DecisionDefinitionModel{}, id); err != nil {
		return err
	}
	m.ID = models.UUID(id)
	if err := db.Save(&m).Error; err != nil {
		return fmt.Errorf("could not update decision: %w", err)
	}
	return nil
}

// Delete removes a decision, refusing an ID outside the caller's tenant.
func (r *gormDecisionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableDecisionDefinitions, &models.DecisionDefinitionModel{}, id); err != nil {
		return err
	}
	if err := db.Delete(&models.DecisionDefinitionModel{}, QualifiedByID(tableDecisionDefinitions), id).Error; err != nil {
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
