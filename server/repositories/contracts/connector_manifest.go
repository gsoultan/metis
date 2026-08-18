package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// ConnectorManifestRepository stores connectors described by a document.
type ConnectorManifestRepository interface {
	// GetByKey returns the manifest a node's connector key names, or a
	// not-found error when no manifest answers to it — which is the ordinary
	// case, because most connectors are still built in.
	GetByKey(ctx context.Context, key string) (models.ConnectorManifestModel, error)

	List(ctx context.Context) ([]models.ConnectorManifestModel, error)

	// Upsert installs a manifest, replacing any earlier one with the same key.
	// Installing is how a connector is *updated*, so a second install of the
	// same key must not be an error.
	Upsert(ctx context.Context, manifest models.ConnectorManifestModel) error

	SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error
	Delete(ctx context.Context, id uuid.UUID) error
}
