package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// AdHocActivator allows knowledge workers to activate any task inside an
// Ad-Hoc SubProcess in any order, any number of times, until the subprocess
// completion condition is satisfied.
type AdHocActivator interface {
	// ActivateTask activates a specific task node inside an ad-hoc subprocess.
	// The caller supplies the instance, the ad-hoc subprocess node ID, and the
	// target task node ID to activate.
	ActivateTask(ctx context.Context, instanceID uuid.UUID, subProcessNodeID string, taskNodeID string) error

	// IsComplete returns true if the ad-hoc subprocess completion condition is met.
	IsComplete(ctx context.Context, instance *entities.ProcessInstance, subProcessNode *entities.Node) (bool, error)
}
