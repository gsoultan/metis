package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// errNoArchive is what every method of a facade built without the archive
// answers, for the reason errNoRetentionHolds gives.
var errNoArchive = errors.New("the archive is unavailable: this server was wired without it")

// unavailableArchive is the Null Object for a facade built without the
// archive service.
type unavailableArchive struct{}

// NewUnavailableArchiveService returns a service that refuses every call, for
// a facade built without the real one.
func NewUnavailableArchiveService() contracts.ArchiveService {
	return unavailableArchive{}
}

func (unavailableArchive) ListArchivedInstances(context.Context, uuid.UUID, repocontracts.ArchiveFilter) ([]models.ArchivedInstanceModel, error) {
	return nil, errNoArchive
}

func (unavailableArchive) GetArchivedInstance(context.Context, uuid.UUID) (models.ArchivedInstanceModel, models.ArchiveRecord, error) {
	return models.ArchivedInstanceModel{}, models.ArchiveRecord{}, errNoArchive
}
