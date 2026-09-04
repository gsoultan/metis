package gorms

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"

	"gorm.io/gorm"
)

type gormDefinitionRepository struct {
	db *gorm.DB
}

// NewDefinitionRepository creates a new GORM-based DefinitionRepository.
func NewDefinitionRepository(db *gorm.DB) contracts.DefinitionRepository {
	return &gormDefinitionRepository{db: db}
}

// Get returns a process definition by ID, scoped to the caller's tenant. The
// row carries the deployed BPMN XML, so an unscoped read hands over another
// organization's process model in full.
func (r *gormDefinitionRepository) Get(ctx context.Context, id uuid.UUID) (models.ProcessDefinitionModel, error) {
	var m models.ProcessDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableProcessDefinitions)
	if err := db.First(&m, QualifiedByID(tableProcessDefinitions), id).Error; err != nil {
		return models.ProcessDefinitionModel{}, lookupError(err, "definition")
	}
	return m, nil
}

// GetByKey returns the latest version of a definition, scoped to the caller's
// tenant. Keys are chosen per project, so two organizations can hold the same
// one — unscoped, this returned whichever happened to sort first, which is a
// leak and a wrong answer at the same time.
func (r *gormDefinitionRepository) GetByKey(ctx context.Context, key string) (models.ProcessDefinitionModel, error) {
	var m models.ProcessDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableProcessDefinitions)
	if err := db.Order(OrderLatestDefinition).Where(ByKey(key)).First(&m).Error; err != nil {
		return models.ProcessDefinitionModel{}, lookupError(err, "definition by key")
	}
	return m, nil
}

// GetByKeyAndVersion pins one version of a definition, scoped to the caller's
// tenant for the same reason GetByKey is.
func (r *gormDefinitionRepository) GetByKeyAndVersion(ctx context.Context, key string, version int) (models.ProcessDefinitionModel, error) {
	var m models.ProcessDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableProcessDefinitions)
	if err := db.Where(ByKeyAndVersion(key, version)).First(&m).Error; err != nil {
		return models.ProcessDefinitionModel{}, lookupError(err, "definition by key and version")
	}
	return m, nil
}

// NextVersion returns the version number a new deployment of key should claim.
//
// See gormDecisionRepository.NextVersion — same contract, same reasoning: the
// count runs over the rows the unique index covers, soft-deleted ones included,
// and the answer is a proposal that the index arbitrates.
func (r *gormDefinitionRepository) NextVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error) {
	db := tenantScopeDB(ctx,
		GetTx(ctx, r.db).Unscoped().Model(&models.ProcessDefinitionModel{}),
		tableProcessDefinitions)

	var highest int
	if err := db.
		Where(QualifiedByProjectID(tableProcessDefinitions), projectID).
		Where(ByKey(key)).
		Select(QueryHighestVersion(tableProcessDefinitions)).
		Scan(&highest).Error; err != nil {
		return 0, fmt.Errorf("could not read the highest definition version: %w", err)
	}
	return highest + 1, nil
}

func (r *gormDefinitionRepository) List(ctx context.Context) ([]models.ProcessDefinitionModel, error) {
	var modelsList []models.ProcessDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_definitions")
	if err := db.Select("process_definitions.id", "process_definitions.project_id", "process_definitions.key", "process_definitions.name", "process_definitions.version", "process_definitions.created_at").Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list definitions: %w", err)
	}
	return modelsList, nil
}

// definitionGraphBatchSize bounds how many definition graphs are held at once
// while scanning. A project keeps every version of every process it has ever
// had, and a graph is the whole BPMN document, so the unbatched form grows
// with installation age — the shape that made BackfillEngineBookkeeping load
// every process instance ever created into memory on every boot.
const definitionGraphBatchSize = 200

// ScanWithGraphs walks the caller's definitions with their node and flow
// graphs hydrated, handing them to visit one batch at a time.
//
// List deliberately projects those columns away, because a list page does not
// need them. A caller that reads what definitions *contain* must ask for them,
// and gets a callback rather than a slice so peak memory is one batch rather
// than the whole installation. Returning an error from visit stops the scan.
func (r *gormDefinitionRepository) ScanWithGraphs(ctx context.Context, visit func([]models.ProcessDefinitionModel) error) error {
	var batch []models.ProcessDefinitionModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_definitions")

	// FindInBatches orders by primary key and pages internally, so the scan is
	// stable and never materializes the full set.
	result := db.FindInBatches(&batch, definitionGraphBatchSize, func(*gorm.DB, int) error {
		return visit(batch)
	})
	if result.Error != nil {
		return fmt.Errorf("could not scan definitions with graphs: %w", result.Error)
	}
	return nil
}

// ListByProjectPaged returns one page of a project's definitions, newest first.
//
// The order is table-qualified: tenant scoping joins the projects table, which
// carries a created_at of its own, and a bare column name is ambiguous the
// moment that join is present — which is every request-driven call.
func (r *gormDefinitionRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p contracts.Pagination) (contracts.Page[models.ProcessDefinitionModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "process_definitions").
		Model(&models.ProcessDefinitionModel{}).
		Where("process_definitions.project_id = ?", projectID)
	return countAndPage[models.ProcessDefinitionModel](base, p, "process_definitions.created_at DESC")
}

func (r *gormDefinitionRepository) Create(ctx context.Context, m models.ProcessDefinitionModel) error {
	// Refuse a process definition planted in another organization's project.
	if err := requireProjectInTenant(ctx, GetTx(ctx, r.db), uuid.UUID(m.ProjectID)); err != nil {
		return err
	}
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return fmt.Errorf("could not create definition: %w", err)
	}
	return nil
}

// tableProcessDefinitions is the SQL table behind ProcessDefinitionModel,
// needed by name so the tenant scope can build its clauses.
const tableProcessDefinitions = "process_definitions"

// Delete removes a process definition, refusing an ID outside the caller's
// tenant.
func (r *gormDefinitionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableProcessDefinitions, &models.ProcessDefinitionModel{}, id); err != nil {
		return err
	}
	if err := db.Delete(&models.ProcessDefinitionModel{}, QualifiedByID(tableProcessDefinitions), id).Error; err != nil {
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
