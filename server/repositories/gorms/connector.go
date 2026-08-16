package gorms

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

type connectorRepository struct {
	db *gorm.DB
}

func NewConnectorRepository(db *gorm.DB) contracts.ConnectorRepository {
	return &connectorRepository{db: db}
}

func (r *connectorRepository) List(ctx context.Context) ([]models.Connector, error) {
	var ms []models.Connector
	if err := ResolveDB(r.db).WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	return ms, nil
}

func (r *connectorRepository) Get(ctx context.Context, id uuid.UUID) (models.Connector, error) {
	var m models.Connector
	if err := ResolveDB(r.db).WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return models.Connector{}, err
	}
	return m, nil
}

func (r *connectorRepository) GetByKey(ctx context.Context, key string) (models.Connector, error) {
	var m models.Connector
	if err := ResolveDB(r.db).WithContext(ctx).Where(ByKey(key)).First(&m).Error; err != nil {
		return models.Connector{}, err
	}
	return m, nil
}

func (r *connectorRepository) Create(ctx context.Context, m models.Connector) (models.Connector, error) {
	if err := ResolveDB(r.db).WithContext(ctx).Create(&m).Error; err != nil {
		return models.Connector{}, err
	}
	return m, nil
}

func (r *connectorRepository) Update(ctx context.Context, m models.Connector) error {
	return ResolveDB(r.db).WithContext(ctx).Save(&m).Error
}

func (r *connectorRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return ResolveDB(r.db).WithContext(ctx).Delete(&models.Connector{}, "id = ?", id).Error
}

type connectorInstanceRepository struct {
	db *gorm.DB
}

func NewConnectorInstanceRepository(db *gorm.DB) contracts.ConnectorInstanceRepository {
	return &connectorInstanceRepository{db: db}
}

// tableConnectorInstances is the SQL table behind ConnectorInstance, needed by
// name so the tenant scope can build its JOIN.
//
// Only the instances are tenant-owned. models.Connector is the shared catalogue
// of connector templates — it carries no project and is the same for everyone,
// so it is correctly left unscoped. The instance is the row that holds the
// configured credentials.
const tableConnectorInstances = "connector_instances"

// ListByProject lists a project's configured connectors, scoped to the caller's
// tenant.
func (r *connectorInstanceRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ConnectorInstance, error) {
	var ms []models.ConnectorInstance
	db := tenantScopeDB(ctx, ResolveDB(r.db).WithContext(ctx), tableConnectorInstances)
	if err := db.Find(&ms, "connector_instances.project_id = ?", projectID).Error; err != nil {
		return nil, err
	}
	return ms, nil
}

// Get returns a configured connector by ID, scoped to the caller's tenant.
// Unscoped, this read hands another organization's stored credentials to
// anyone who can guess a UUID.
func (r *connectorInstanceRepository) Get(ctx context.Context, id uuid.UUID) (models.ConnectorInstance, error) {
	var m models.ConnectorInstance
	db := tenantScopeDB(ctx, ResolveDB(r.db).WithContext(ctx), tableConnectorInstances)
	if err := db.First(&m, QualifiedByID(tableConnectorInstances), id).Error; err != nil {
		return models.ConnectorInstance{}, err
	}
	return m, nil
}

// GetByProjectAndConnector resolves the connector a project configured for a
// given template, scoped to the caller's tenant.
func (r *connectorInstanceRepository) GetByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (models.ConnectorInstance, error) {
	var m models.ConnectorInstance
	db := tenantScopeDB(ctx, ResolveDB(r.db).WithContext(ctx), tableConnectorInstances)
	if err := db.First(&m, "connector_instances.project_id = ? AND connector_instances.connector_id = ?",
		projectID, connectorID).Error; err != nil {
		return models.ConnectorInstance{}, err
	}
	return m, nil
}

func (r *connectorInstanceRepository) Create(ctx context.Context, m models.ConnectorInstance) (models.ConnectorInstance, error) {
	if err := ResolveDB(r.db).WithContext(ctx).Create(&m).Error; err != nil {
		return models.ConnectorInstance{}, err
	}
	return m, nil
}

// Update saves a configured connector, refusing an ID outside the caller's
// tenant — otherwise another organization's stored credentials could be
// overwritten, or repointed at a host the attacker controls.
func (r *connectorInstanceRepository) Update(ctx context.Context, m models.ConnectorInstance) error {
	db := ResolveDB(r.db).WithContext(ctx)
	if err := requireVisibleToTenant(ctx, db, tableConnectorInstances, &models.ConnectorInstance{}, uuid.UUID(m.ID)); err != nil {
		return err
	}
	return db.Save(&m).Error
}

// Delete removes a configured connector, refusing an ID outside the caller's
// tenant.
func (r *connectorInstanceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := ResolveDB(r.db).WithContext(ctx)
	if err := requireVisibleToTenant(ctx, db, tableConnectorInstances, &models.ConnectorInstance{}, id); err != nil {
		return err
	}
	return db.Delete(&models.ConnectorInstance{}, QualifiedByID(tableConnectorInstances), id).Error
}
