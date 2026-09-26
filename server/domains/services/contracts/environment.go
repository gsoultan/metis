package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// EnvironmentService manages the runtimes a project deploys into.
//
// Every read here returns the database connection with its credentials masked;
// an update accepts the mask back to mean "unchanged". A database password is
// worth more than any single connector's token — it is every credential in that
// runtime at once — so it never reaches a browser.
type EnvironmentService interface {
	ListEnvironments(ctx context.Context, projectID uuid.UUID) ([]entities.Environment, error)
	GetEnvironment(ctx context.Context, id uuid.UUID) (entities.Environment, error)
	CreateEnvironment(ctx context.Context, environment entities.Environment) (uuid.UUID, error)
	UpdateEnvironment(ctx context.Context, environment entities.Environment) error

	// DeleteEnvironment removes the runtime from the registry. The database it
	// named is left alone.
	DeleteEnvironment(ctx context.Context, id uuid.UUID) error

	// TestConnection opens the described database and reports whether it
	// answered, so an administrator finds out before saving rather than from
	// the server's log once every replica tries to open it.
	//
	// It takes a host and a port from the caller and says what happened to the
	// attempt, which is a network probe. That is why it is administrative: the
	// setup wizard's equivalent is public, and closes as soon as the
	// installation is configured.
	TestEnvironmentConnection(ctx context.Context, environment entities.Environment) entities.EnvironmentHealth
}
