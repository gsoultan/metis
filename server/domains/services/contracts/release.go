package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// CutRelease asks for a release of a project's live models in the runtime the
// work is bound to.
type CutRelease struct {
	ProjectID uuid.UUID
	Name      string
	// Processes are the process keys to carry. The decisions they consult and
	// the processes they call are carried with them.
	Processes []string
	// Decisions are decision keys to carry besides those.
	Decisions []string
}

// ReleaseService moves process and decision versions from one environment to
// the next together, unchanged.
//
// A release is cut where it was designed and tested, imported into the next
// runtime — stored there, checked, not live — and then made live there in one
// step. Every method works on the runtime the work is bound to: the port the
// request arrived on.
type ReleaseService interface {
	// CutRelease records the live versions of the named models here as a
	// release. Nothing that runs changes.
	CutRelease(ctx context.Context, req CutRelease) (entities.Release, error)

	// ImportReleaseFrom imports a release cut in another environment of the
	// same project — or in the main runtime, when sourceEnvironment is
	// uuid.Nil — into this one, staged. Importing the same release twice
	// answers the first import.
	ImportReleaseFrom(ctx context.Context, projectID, sourceEnvironment uuid.UUID, name string) (entities.Release, error)

	// GoLive makes every model of a staged release live here, together.
	GoLive(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error)

	// RollBack makes the release that was live before the live one live
	// again.
	RollBack(ctx context.Context, projectID uuid.UUID) (entities.Release, error)

	// ListReleases lists a project's releases here, newest first.
	ListReleases(ctx context.Context, projectID uuid.UUID) ([]entities.Release, error)

	// GetRelease reads one release here.
	GetRelease(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error)
}
