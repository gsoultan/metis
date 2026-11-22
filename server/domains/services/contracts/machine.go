package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// MachineService is the Servers page: the processes running this
// installation, which share its database, as each last said it was.
type MachineService interface {
	// ListMachines is every process that said it was running, the most
	// recently heard first. A platform administrator's.
	ListMachines(ctx context.Context) ([]entities.Machine, error)
	// ForgetMachine removes a process that stopped saying it was running.
	// One heard from in the last two minutes is refused: it is running, and
	// would only come back. A platform administrator's.
	ForgetMachine(ctx context.Context, id uuid.UUID) error
	// StartMachineHeartbeat says, every few seconds until ctx ends, that this
	// process is running and what it is. Every process does, whatever its
	// role.
	StartMachineHeartbeat(ctx context.Context, self entities.Machine)
}
