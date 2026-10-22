package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// errNoBatchOperations is what every method of a facade built without the
// batch operation service answers: a plain error, because it is how the
// server was put together.
var errNoBatchOperations = errors.New("batch operations are unavailable: this server was wired without them")

// unavailableBatchOperations is the Null Object for a facade built without
// the batch operation service.
type unavailableBatchOperations struct{}

// NewUnavailableBatchOperationService returns a service that refuses every
// call and starts no worker, for a facade built without the real one.
func NewUnavailableBatchOperationService() contracts.BatchOperationService {
	return unavailableBatchOperations{}
}

func (unavailableBatchOperations) StartBatchOperation(context.Context, entities.BatchOperationRequest) (entities.BatchOperationOutcome, error) {
	return entities.BatchOperationOutcome{}, errNoBatchOperations
}

func (unavailableBatchOperations) BatchOperation(context.Context, uuid.UUID) (entities.BatchOperation, error) {
	return entities.BatchOperation{}, errNoBatchOperations
}

func (unavailableBatchOperations) ListBatchOperations(context.Context, uuid.UUID) ([]entities.BatchOperation, error) {
	return nil, errNoBatchOperations
}

func (unavailableBatchOperations) BatchOperationItems(context.Context, uuid.UUID, entities.BatchItemStatus, repocontracts.Pagination) (repocontracts.Page[entities.BatchItem], error) {
	return repocontracts.Page[entities.BatchItem]{}, errNoBatchOperations
}

func (unavailableBatchOperations) StopBatchOperation(context.Context, uuid.UUID, string) (entities.BatchOperation, error) {
	return entities.BatchOperation{}, errNoBatchOperations
}

func (unavailableBatchOperations) StartBatchOperations(context.Context) {}
