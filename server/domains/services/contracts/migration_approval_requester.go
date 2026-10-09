package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// MigrationApprovalRequester asks a second administrator to approve a
// migration whose plan says it needs one (MigrationPlan.RequiresSecondApprover):
// it skips a step, takes a control from instances that have not passed it,
// redirects a step past such a control, or loosens a separation-of-duties
// rule on a step some instance has still to pass.
type MigrationApprovalRequester interface {
	// RequestMigrationApproval records the migration as a request that waits,
	// and moves nothing. Asked again by whoever asked, for the same
	// migration, it answers the request that already waits. A migration that
	// needs nobody else is refused: it is applied, not asked for.
	RequestMigrationApproval(ctx context.Context, sourceDefID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...MigrationOption) (entities.PendingApproval, error)
}
