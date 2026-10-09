package impl

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// errNoDeviationRequestService is what every question about a request for a
// second administrator answers on a server put together without the service
// of them. A plain error: it is how the server was wired, and nothing a
// caller sent.
var errNoDeviationRequestService = errors.New(
	"this deployment was wired without the service of requests for a second administrator")

// errNoInstanceDeviator is the same answer for the in-place command.
var errNoInstanceDeviator = errors.New(
	"this deployment was wired without the service that waives, cancels and holds an instance in place")

// unavailableDeviationRequests is the service of requests of a wiring that
// has none. It refuses every method, and says why. The alternative is a nil
// interface where the service should be: a call through one is a nil
// dereference, not an answer — and a list that answered "nothing waits"
// would pass for a server on which nothing needs anybody's approval.
type unavailableDeviationRequests struct{}

func (unavailableDeviationRequests) ApproveDeviationRequest(context.Context, uuid.UUID, string) (entities.DeviationRequestOutcome, error) {
	return entities.DeviationRequestOutcome{}, errNoDeviationRequestService
}

func (unavailableDeviationRequests) RejectDeviationRequest(context.Context, uuid.UUID, string) (entities.DeviationRequest, error) {
	return entities.DeviationRequest{}, errNoDeviationRequestService
}

func (unavailableDeviationRequests) ListDeviationRequests(context.Context, entities.DeviationRequestQuery) ([]entities.DeviationRequest, int64, error) {
	return nil, 0, errNoDeviationRequestService
}

func (unavailableDeviationRequests) GetDeviationRequest(context.Context, uuid.UUID) (entities.DeviationRequest, error) {
	return entities.DeviationRequest{}, errNoDeviationRequestService
}

func (unavailableDeviationRequests) ExpireDeviationRequests(context.Context, time.Time) (int64, error) {
	return 0, errNoDeviationRequestService
}

// unavailableDeviator is the in-place command of a wiring that has none.
type unavailableDeviator struct{}

func (unavailableDeviator) DeviateInstance(context.Context, entities.DeviationCommand) (entities.DeviationOutcome, error) {
	return entities.DeviationOutcome{}, errNoInstanceDeviator
}
