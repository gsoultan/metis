package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationRequestReader reads requests without locking them. Both reads are
// scoped to the caller's organization: another organization's request is not
// found.
type DeviationRequestReader interface {
	// Get answers one request, whole and as stored. Its Status is the stored
	// one; what it reads as at a given moment is EffectiveStatus.
	Get(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)

	// GetReadable answers one request as stored, with each of its sealed
	// documents that opens. A document that does not open is left nil —
	// Command, Plan or ApprovedInstances, which a whole request never has
	// nil — where Get would fail. It is for whoever has to see a request, or
	// find out whether it still waits, without needing all of what it asked:
	// a request whose plan no longer opens is still one somebody has to be
	// able to read, reject and close. Whoever acts on what a request asked
	// for reads it whole.
	GetReadable(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)

	// List answers one page of the caller's requests, newest first, and how
	// many there are in all. A status in the query is the status a request
	// reads as at now (EffectiveStatus): a waiting request past its deadline
	// is listed as expired and not as pending, whatever the table says. Each
	// row's Status is still the stored one.
	//
	// A listed request has no Command, no Plan and no ApprovedInstances —
	// they are nil, and so is what Because reads from the plan. Those three
	// are the heavy part of a request, one of them without a bound, and a
	// page does not read them: Get does, one request at a time.
	List(ctx context.Context, query entities.DeviationRequestQuery, now time.Time) ([]entities.DeviationRequest, int64, error)
}
