package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ConnectorReader provides read access to connector templates.
type ConnectorReader interface {
	ListConnectors(ctx context.Context) ([]entities.Connector, error)
	GetConnector(ctx context.Context, id uuid.UUID) (entities.Connector, error)
}

// ConnectorWriter provides write access to connector templates.
type ConnectorWriter interface {
	CreateConnector(ctx context.Context, connector entities.Connector) (entities.Connector, error)
	UpdateConnector(ctx context.Context, connector entities.Connector) error
	DeleteConnector(ctx context.Context, id uuid.UUID) error

	// EnsureDefaultConnectors creates the built-in connector catalogue in the
	// database that is current now, skipping any that already exist.
	//
	// It has to be callable rather than only running at construction because
	// the first run writes into the bootstrap database and setup then swaps to
	// the real one, which would otherwise be left with an empty catalogue for
	// good — no Slack, no email, no HTTP for any service task to pick.
	EnsureDefaultConnectors(ctx context.Context) error
}

// ConnectorInstanceManager manages project-scoped connector instances.
type ConnectorInstanceManager interface {
	ListConnectorInstances(ctx context.Context, projectID uuid.UUID) ([]entities.ConnectorInstance, error)
	GetConnectorInstance(ctx context.Context, id uuid.UUID) (entities.ConnectorInstance, error)
	GetConnectorInstanceByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (entities.ConnectorInstance, error)
	CreateConnectorInstance(ctx context.Context, instance entities.ConnectorInstance) (entities.ConnectorInstance, error)
	UpdateConnectorInstance(ctx context.Context, instance entities.ConnectorInstance) error
	DeleteConnectorInstance(ctx context.Context, id uuid.UUID) error
}

// ConnectorRegistry manages registration and execution of connector executors.
type ConnectorRegistry interface {
	RegisterExecutor(key string, executor ConnectorExecutor)
	ExecuteConnector(ctx context.Context, connectorKey string, config map[string]any, payload map[string]any) (map[string]any, error)
}

// ConnectorService composes all connector operations into the full service contract.
type ConnectorService interface {
	ConnectorReader
	ConnectorWriter
	ConnectorInstanceManager
	ConnectorRegistry
	ConnectorManifestManager
}

// ConnectorManifestManager installs connectors described by a document rather
// than compiled in.
//
// On the interface rather than only on the implementation, which is where these
// began: a method that exists only on the concrete type is a method nothing
// holding the interface can call, and everything here holds the interface.
type ConnectorManifestManager interface {
	// InstallManifest validates and stores a manifest. Installing an existing
	// key replaces it, because installing again is how an author fixes one.
	InstallManifest(ctx context.Context, document []byte) (entities.ConnectorManifest, error)

	// ImportOpenAPI turns a specification into manifests and installs them all.
	ImportOpenAPI(ctx context.Context, document []byte) ([]entities.ConnectorManifest, error)

	ListManifests(ctx context.Context) ([]entities.ConnectorManifest, error)

	// GetManifestDocument returns a manifest as its author wrote it.
	GetManifestDocument(ctx context.Context, key string) (string, error)

	SetManifestEnabled(ctx context.Context, id uuid.UUID, enabled bool) error
	DeleteManifest(ctx context.Context, id uuid.UUID) error
}

// ConnectorExecutor defines the Strategy interface for a specific connector's execution logic.
// Each built-in or custom connector implements this and registers via ConnectorRegistry.
type ConnectorExecutor interface {
	Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error)
}
