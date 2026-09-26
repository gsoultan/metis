package impl

import (
	"cmp"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The catalogue entry an installed manifest brings with it.
//
// A step reaches a manifest only through a catalogue entry. The designer offers
// the entries; findConnectorInstance resolves a step's connector_id to the
// project's connection for one; the job reads that entry's key; and
// ExecuteConnectorRequest finds the manifest by the key. So installing writes
// an entry, rather than the catalogue listing manifests as it reads: a
// connection is a row naming an entry by id, and a manifest listed but not
// stored would be one nothing could connect or resolve at run time.
//
// The entry is offered while the manifest is installed and switched on.
// Switching it off or removing it withdraws the entry — marked removed, not
// deleted, so the steps and connections that name it keep their reference — and
// switching it back on or installing it again brings the same entry back.
//
// A built-in's key is left alone. Its entry is the built-in's: a manifest under
// that key changes what the call does, and when the manifest is switched off or
// removed the built-in answers again, under the entry that was always there.

// syncCatalogue offers manifest in the catalogue while it is switched on, and
// withdraws it while it is not.
func (s *connectorService) syncCatalogue(ctx context.Context, manifest connectors.Manifest, enabled bool) error {
	if s.isBuiltIn(manifest.Key) {
		return nil
	}
	if !enabled {
		return s.withdrawFromCatalogue(ctx, manifest.Key)
	}
	return s.offerInCatalogue(ctx, manifest.CatalogueEntry())
}

// syncCatalogueFrom is syncCatalogue for a stored manifest. Withdrawing needs
// only the key, so a document that no longer parses can still be switched off
// or removed; offering needs the document read.
func (s *connectorService) syncCatalogueFrom(ctx context.Context, stored models.ConnectorManifestModel, enabled bool) error {
	if !enabled {
		return s.syncCatalogue(ctx, connectors.Manifest{Key: stored.Key}, false)
	}
	manifest, err := connectors.ParseManifest([]byte(stored.Document))
	if err != nil {
		return apierr.Invalidf("the installed document for %q can no longer be read, so it cannot be switched on; "+
			"install it again: %v", stored.Key, err)
	}
	return s.syncCatalogue(ctx, manifest, true)
}

// offerInCatalogue writes entry over the one its key has — live, or brought
// back from being withdrawn — and creates one only for a key never offered.
//
// What the manifest says wins: its name, its category and the settings it
// reads. It has no description, so a description the entry already carries is
// kept, and so is an icon when the manifest names none.
func (s *connectorService) offerInCatalogue(ctx context.Context, entry entities.Connector) error {
	current, found, err := s.catalogueEntryFor(ctx, entry.Key)
	if err != nil {
		return err
	}
	if !found {
		_, err := s.CreateConnector(ctx, entry)
		return err
	}
	entry.ID = current.ID
	entry.Description = cmp.Or(entry.Description, current.Description)
	entry.Icon = cmp.Or(entry.Icon, current.Icon)
	return s.UpdateConnector(ctx, entry)
}

// catalogueEntryFor finds the entry for key: the live one, or else the one
// withdrawn last, which it brings back.
func (s *connectorService) catalogueEntryFor(ctx context.Context, key string) (entities.Connector, bool, error) {
	live, err := s.repo.Connector().GetByKey(ctx, key)
	if err == nil {
		return adapters.ConnectorEntityAdapter{Model: live}.ToEntity(), true, nil
	}
	if !errors.Is(err, apierr.ErrNotFound) {
		return entities.Connector{}, false, err
	}
	restored, err := s.repo.Connector().RestoreByKey(ctx, key)
	switch {
	case err == nil:
		return adapters.ConnectorEntityAdapter{Model: restored}.ToEntity(), true, nil
	case errors.Is(err, apierr.ErrNotFound):
		return entities.Connector{}, false, nil
	default:
		return entities.Connector{}, false, err
	}
}

func (s *connectorService) withdrawFromCatalogue(ctx context.Context, key string) error {
	live, err := s.repo.Connector().GetByKey(ctx, key)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.repo.Connector().Delete(ctx, uuid.UUID(live.ID))
}
