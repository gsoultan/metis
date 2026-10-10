package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationRequestLister reads the requests that wait, or waited, for a
// second administrator. Both reads are an administrator's, in the
// organization the request is for, and answer each request with the status it
// has now: one past its deadline reads as expired whether or not anything has
// written that down.
type DeviationRequestLister interface {
	// ListDeviationRequests answers one page of requests, newest first, and
	// how many there are in all. With no status in the query it lists what
	// still waits. A listed request carries no command and no plan: those are
	// read one request at a time.
	ListDeviationRequests(ctx context.Context, query entities.DeviationRequestQuery) ([]entities.DeviationRequest, int64, error)

	// GetDeviationRequest answers one request, whole.
	GetDeviationRequest(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)
}
