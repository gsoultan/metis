package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ReleaseRepository keeps the releases one runtime holds: the ones cut there
// and the ones imported into it. Every method is scoped to the caller's
// organization, and reads and writes the runtime the work is bound to.
type ReleaseRepository interface {
	// Create records a release. A name the project already has here is
	// refused as a conflict.
	Create(ctx context.Context, r entities.Release) (entities.Release, error)
	// GetByName reads one release of a project.
	GetByName(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error)
	// GetLive reads the project's live release here, or ErrNotFound when none
	// has gone live.
	GetLive(ctx context.Context, projectID uuid.UUID) (entities.Release, error)
	// List reads a project's releases, newest first, at most limit of them.
	List(ctx context.Context, projectID uuid.UUID, limit int) ([]entities.Release, error)
	// Update rewrites a release's state, items and who last acted on it.
	Update(ctx context.Context, r entities.Release) error
}
