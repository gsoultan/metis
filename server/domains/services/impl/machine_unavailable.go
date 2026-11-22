package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

var errNoMachines = errors.New("the list of machines is not available in this process")

type unavailableMachines struct{}

// NewUnavailableMachineService refuses every call, for a facade built
// without a database.
func NewUnavailableMachineService() contracts.MachineService { return unavailableMachines{} }

func (unavailableMachines) ListMachines(context.Context) ([]entities.Machine, error) {
	return nil, errNoMachines
}

func (unavailableMachines) ForgetMachine(context.Context, uuid.UUID) error { return errNoMachines }

func (unavailableMachines) StartMachineHeartbeat(context.Context, entities.Machine) {}
