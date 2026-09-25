package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// install writes one parsed manifest over whatever is installed under its key.
//
// It runs inside the caller's transaction. What it writes depends on what is
// already there, so the installed row is read and held first.
func (s *connectorService) install(ctx context.Context, manifest connectors.Manifest, document []byte) (entities.ConnectorManifest, error) {
	store := s.repo.ConnectorManifest()
	current, installed, err := installedManifest(ctx, store, manifest.Key)
	if err != nil {
		return entities.ConnectorManifest{}, err
	}

	if err := store.Upsert(ctx, models.ConnectorManifestModel{
		Key:      manifest.Key,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Document: string(document),
		// A new manifest is switched on: installing a connector is asking for
		// it. One already installed keeps its switch. Installing again is how
		// an author fixes a document, and an administrator who switched a
		// connector off — the partner asked us to stop calling it — has not
		// asked for it back on by fixing it.
		Enabled: !installed || current.Enabled,
	}); err != nil {
		return entities.ConnectorManifest{}, err
	}

	// Read back rather than returning what was sent. Installing an existing key
	// keeps that row's id, so the id the caller needs — to switch this
	// connector off, or delete it — is the stored one and not the one this
	// function might have generated.
	stored, err := store.GetByKey(ctx, manifest.Key)
	if err != nil {
		return entities.ConnectorManifest{}, err
	}
	return manifestEntity(stored), nil
}

// installedManifest reads what is installed under key, holding its row until
// the transaction ends. A switch-off landing between this read and the write
// it decides would otherwise be undone by that write.
func installedManifest(ctx context.Context, store repocontracts.ConnectorManifestRepository, key string) (models.ConnectorManifestModel, bool, error) {
	current, err := store.GetByKeyForUpdate(ctx, key)
	switch {
	case err == nil:
		return current, true, nil
	case errors.Is(err, apierr.ErrNotFound):
		return models.ConnectorManifestModel{}, false, nil
	default:
		return models.ConnectorManifestModel{}, false, err
	}
}

// manifestEntity is a stored manifest as the catalogue lists it: without its
// document.
func manifestEntity(m models.ConnectorManifestModel) entities.ConnectorManifest {
	return entities.ConnectorManifest{
		ID: uuid.UUID(m.ID), Key: m.Key, Name: m.Name, Version: m.Version, Enabled: m.Enabled,
	}
}
