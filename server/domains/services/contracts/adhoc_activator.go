package contracts

import (
	"context"

	"github.com/google/uuid"
)

// AdHocActivator allows knowledge workers to activate any task inside an
// Ad-Hoc SubProcess in any order, any number of times, until the subprocess
// completion condition is satisfied.
type AdHocActivator interface {
	// ActivateTask activates a specific task node inside an ad-hoc subprocess.
	// The caller supplies the instance, the ad-hoc subprocess node ID, and the
	// target task node ID to activate. Who started the step is entered in the
	// instance's deviation ledger and on its trail, with the reason an option
	// gives; an activation that cannot be recorded is not made.
	ActivateTask(ctx context.Context, instanceID uuid.UUID, subProcessNodeID string, taskNodeID string, opts ...ActivationOption) error
}
