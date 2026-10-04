package impl

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// parkedWorkWithdrawer is the engine's withdrawal of work parked for outside
// workers: the one implementation of it, which a cancel reuses.
type parkedWorkWithdrawer interface {
	withdrawExternalTasksOn(ctx context.Context, instance *entities.ProcessInstance, nodes []*entities.Node) error
}
