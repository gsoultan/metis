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

func (unavailableMachines) ChangeMachineRole(context.Context, uuid.UUID, string) error {
	return errNoMachines
}

func (unavailableMachines) StartMachineHeartbeat(context.Context, entities.Machine, func(string)) {}

func (unavailableMachines) MakeMachineJoin(context.Context, string) (contracts.IssuedMachineJoin, error) {
	return contracts.IssuedMachineJoin{}, errNoMachines
}

func (unavailableMachines) ListMachineJoins(context.Context) ([]entities.MachineJoin, error) {
	return nil, errNoMachines
}

func (unavailableMachines) AcceptMachineJoin(context.Context, uuid.UUID) (entities.MachineJoin, error) {
	return entities.MachineJoin{}, errNoMachines
}

func (unavailableMachines) RemoveMachineJoin(context.Context, uuid.UUID) error { return errNoMachines }

func (unavailableMachines) RequestMachineJoin(context.Context, contracts.MachineJoinRequest) (contracts.MachineJoinReply, error) {
	return contracts.MachineJoinReply{}, errNoMachines
}

func (unavailableMachines) CollectMachineJoin(context.Context, string) (contracts.MachineJoinCollection, error) {
	return contracts.MachineJoinCollection{}, errNoMachines
}
