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

func (r *connectorInstanceRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ConnectorInstance, error) {
	var ms []models.ConnectorInstance
	if err := ResolveDB(r.db).WithContext(ctx).Find(&ms, "project_id = ?", projectID).Error; err != nil {
		return nil, err
	}
	return ms, nil
}

func (r *connectorInstanceRepository) Get(ctx context.Context, id uuid.UUID) (models.ConnectorInstance, error) {
	var m models.ConnectorInstance
	if err := ResolveDB(r.db).WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return models.ConnectorInstance{}, err
	}
	return m, nil
}

func (r *connectorInstanceRepository) GetByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (models.ConnectorInstance, error) {
	var m models.ConnectorInstance
	if err := ResolveDB(r.db).WithContext(ctx).First(&m, "project_id = ? AND connector_id = ?", projectID, connectorID).Error; err != nil {
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

func (r *connectorInstanceRepository) Update(ctx context.Context, m models.ConnectorInstance) error {
	return ResolveDB(r.db).WithContext(ctx).Save(&m).Error
}

func (r *connectorInstanceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return ResolveDB(r.db).WithContext(ctx).Delete(&models.ConnectorInstance{}, "id = ?", id).Error
}
