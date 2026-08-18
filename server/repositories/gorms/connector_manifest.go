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

const tableConnectorManifests = "connector_manifests"

type gormConnectorManifestRepository struct {
	db *gorm.DB
}

// NewConnectorManifestRepository creates a new GORM-based repository.
func NewConnectorManifestRepository(db *gorm.DB) contracts.ConnectorManifestRepository {
	return &gormConnectorManifestRepository{db: db}
}

// GetByKey finds the manifest for a connector key.
//
// Not tenant-scoped, matching the built-in connectors it sits alongside: a
// connector is a *definition* — what Salesforce's API looks like — not one
// organization's data. What is per-tenant is the connector instance holding the
// credentials, and that is scoped already.
func (r *gormConnectorManifestRepository) GetByKey(ctx context.Context, key string) (models.ConnectorManifestModel, error) {
	var m models.ConnectorManifestModel
	if err := GetTx(ctx, r.db).Where(ByKey(key)).First(&m).Error; err != nil {
		return models.ConnectorManifestModel{}, fmt.Errorf("could not get connector manifest: %w", err)
	}
	return m, nil
}

func (r *gormConnectorManifestRepository) List(ctx context.Context) ([]models.ConnectorManifestModel, error) {
	var list []models.ConnectorManifestModel
	if err := GetTx(ctx, r.db).Order("key").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("could not list connector manifests: %w", err)
	}
	return list, nil
}

// Upsert installs a manifest, replacing any earlier one with the same key.
//
// Installing is how a manifest is updated — an author fixes a URL and installs
// again — so a second install of the same key replaces rather than failing.
func (r *gormConnectorManifestRepository) Upsert(ctx context.Context, m models.ConnectorManifestModel) error {
	if m.ID == models.NilUUID {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("could not generate a manifest id: %w", err)
		}
		m.ID = models.UUID(id)
	}
	err := GetTx(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "version", "document", "enabled", "updated_at"}),
	}).Create(&m).Error
	if err != nil {
		return fmt.Errorf("could not install connector manifest: %w", err)
	}
	return nil
}

func (r *gormConnectorManifestRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	result := GetTx(ctx, r.db).Model(&models.ConnectorManifestModel{}).
		Where(QualifiedByID(tableConnectorManifests), id).
		Update("enabled", enabled)
	if result.Error != nil {
		return fmt.Errorf("could not switch the connector manifest: %w", result.Error)
	}
	// An update that matched nothing is not a success. Returning nil here is how
	// "I switched that connector off" becomes "nothing happened, and nobody was
	// told" — which is exactly what an all-zero id did before the caller was
	// fixed to read one back.
	if result.RowsAffected == 0 {
		return fmt.Errorf("no connector manifest with id %s", id)
	}
	return nil
}

func (r *gormConnectorManifestRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := GetTx(ctx, r.db).
		Delete(&models.ConnectorManifestModel{}, QualifiedByID(tableConnectorManifests), id)
	if result.Error != nil {
		return fmt.Errorf("could not delete connector manifest: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("no connector manifest with id %s", id)
	}
	return nil
}
