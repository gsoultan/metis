package impl

import (
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// allowBuiltInOverrideEnv lets a manifest take the key of a connector built
// into Metis.
//
// Such a manifest replaces the built-in in every step that uses it, in every
// organization on the installation, whoever wrote the step. That is how a
// built-in is replaced without a redeploy, and it is also how one organization
// would take over a connector all of them rely on — so it is the operator's
// decision, off unless they make it.
const allowBuiltInOverrideEnv = "METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE"

// refuseBuiltInKey stops a manifest taking a built-in's key unless the
// operator allows it. Refused rather than installed under another key, so
// whoever installed it hears why nothing changed.
func (s *connectorService) refuseBuiltInKey(key string) error {
	if !s.isBuiltIn(key) || builtInOverrideAllowed() {
		return nil
	}
	return apierr.Invalidf(
		"%q is the key of a connector built into Metis, and a manifest under it would replace that connector in "+
			"every step, in every organization, that uses it; give the manifest a key of its own, or ask whoever "+
			"operates this installation to set %s=true", key, allowBuiltInOverrideEnv)
}

func builtInOverrideAllowed() bool {
	allowed, err := strconv.ParseBool(envvar.Get(allowBuiltInOverrideEnv))
	return err == nil && allowed
}

// isBuiltIn reports whether a compiled-in executor answers key.
func (s *connectorService) isBuiltIn(key string) bool {
	_, builtIn := s.executors[key]
	return builtIn
}

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
	if installed {
		if err := refuseDowngrade(current, manifest); err != nil {
			return entities.ConnectorManifest{}, err
		}
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
	// In the same transaction, so a manifest is never installed without the
	// catalogue entry a step needs to reach it, nor offered with a stale form.
	if err := s.syncCatalogue(ctx, manifest, stored.Enabled); err != nil {
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

// refuseDowngrade stops an older document replacing a newer one.
//
// Installing over a manifest is how it is fixed, so the same version is
// allowed, and a later one is an upgrade. An earlier one is almost always a
// stale copy, and installing it quietly takes every step that names the key
// back to behaviour somebody had moved on from. Refused rather than skipped,
// so whoever installed it hears why nothing changed.
func refuseDowngrade(current models.ConnectorManifestModel, incoming connectors.Manifest) error {
	if incoming.Version >= current.Version {
		return nil
	}
	return apierr.Invalidf(
		"%q is installed at version %d, and this document is version %d; installing it would put back "+
			"an older connector. Install version %d or later to change it",
		incoming.Key, current.Version, incoming.Version, current.Version)
}

// manifestEntity is a stored manifest as the catalogue lists it: without its
// document.
func manifestEntity(m models.ConnectorManifestModel) entities.ConnectorManifest {
	return entities.ConnectorManifest{
		ID: uuid.UUID(m.ID), Key: m.Key, Name: m.Name, Version: m.Version, Enabled: m.Enabled,
	}
}
