package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// errNoSimulationCases is what every method of a facade built without the
// simulation case service answers, for the reason errNoRetentionHolds gives.
var errNoSimulationCases = errors.New("saved simulation cases are unavailable: this server was wired without them")

// unavailableSimulationCases is the Null Object for a facade built without
// the simulation case service.
type unavailableSimulationCases struct{}

// NewUnavailableSimulationCaseService returns a service that refuses every
// call, for a facade built without the real one.
func NewUnavailableSimulationCaseService() contracts.SimulationCaseService {
	return unavailableSimulationCases{}
}

func (unavailableSimulationCases) ListSimulationCases(context.Context, uuid.UUID, string) ([]entities.SimulationCase, error) {
	return nil, errNoSimulationCases
}

func (unavailableSimulationCases) SaveSimulationCase(context.Context, entities.SimulationCase) (entities.SimulationCase, error) {
	return entities.SimulationCase{}, errNoSimulationCases
}

func (unavailableSimulationCases) DeleteSimulationCase(context.Context, uuid.UUID) error {
	return errNoSimulationCases
}
