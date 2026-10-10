package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationApprover decides a request that waits for a second administrator.
// Whoever decides is a signed-in administrator of the organization the
// request belongs to; an administrator of another is told there is no such
// request.
type DeviationApprover interface {
	// ApproveDeviationRequest approves a request and carries out what it
	// asked for, as the instance or the version stands now. Whoever asked
	// does not approve their own request. A request that no longer holds is
	// recorded as stale, one past its deadline as expired, and either is
	// refused; reason is the approver's note, and may be empty.
	ApproveDeviationRequest(ctx context.Context, id uuid.UUID, reason string) (entities.DeviationRequestOutcome, error)

	// RejectDeviationRequest ends a request without carrying it out, and
	// answers it as it then is. The reason is required.
	RejectDeviationRequest(ctx context.Context, id uuid.UUID, reason string) (entities.DeviationRequest, error)
}
