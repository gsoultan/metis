package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// RetentionHoldService keeps instances out of the finished-instance sweep:
// a legal hold, an investigation, an auditor's request for a case to be kept.
//
// Placing and releasing are an administrator's of the instance's
// organization, each with a reason, and each is written to the instance's
// audit trail with the hold itself. Reading is open to whoever may read the
// instance.
type RetentionHoldService interface {
	// HoldInstance keeps an instance, and the whole case of calls it is part
	// of, out of the sweep until the hold is released.
	HoldInstance(ctx context.Context, instanceID uuid.UUID, reason string) (entities.RetentionHold, error)
	// ReleaseInstanceHold releases the hold on an instance and returns what it
	// was. The instance is then swept like any other once its period is over.
	ReleaseInstanceHold(ctx context.Context, instanceID uuid.UUID, reason string) (entities.RetentionHold, error)
	// InstanceHold reads the hold on an instance; held is false when there is
	// none.
	InstanceHold(ctx context.Context, instanceID uuid.UUID) (hold entities.RetentionHold, held bool, err error)
	// ListRetentionHolds lists a project's holds, newest first.
	ListRetentionHolds(ctx context.Context, projectID uuid.UUID) ([]entities.RetentionHold, error)
}
