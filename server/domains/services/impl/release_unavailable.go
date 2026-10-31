package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// errNoReleases is what every method of a facade built without the release
// service answers.
var errNoReleases = errors.New("releases are unavailable: this server was wired without them")

// unavailableReleases is the Null Object for a facade built without the
// release service.
type unavailableReleases struct{}

// NewUnavailableReleaseService returns a service that refuses every call.
func NewUnavailableReleaseService() contracts.ReleaseService { return unavailableReleases{} }

func (unavailableReleases) CutRelease(context.Context, contracts.CutRelease) (entities.Release, error) {
	return entities.Release{}, errNoReleases
}

func (unavailableReleases) ImportReleaseFrom(context.Context, uuid.UUID, uuid.UUID, string) (entities.Release, error) {
	return entities.Release{}, errNoReleases
}

func (unavailableReleases) GoLive(context.Context, uuid.UUID, string) (entities.Release, error) {
	return entities.Release{}, errNoReleases
}

func (unavailableReleases) RollBack(context.Context, uuid.UUID) (entities.Release, error) {
	return entities.Release{}, errNoReleases
}

func (unavailableReleases) ListReleases(context.Context, uuid.UUID) ([]entities.Release, error) {
	return nil, errNoReleases
}

func (unavailableReleases) GetRelease(context.Context, uuid.UUID, string) (entities.Release, error) {
	return entities.Release{}, errNoReleases
}
