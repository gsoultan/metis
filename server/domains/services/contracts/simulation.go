package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// SimulationService runs a deployed definition on the real engine, against a
// virtual clock, persisting nothing and calling nothing.
//
// One method, deliberately. The whole trace comes back in one response rather
// than a step at a time, which is what lets a client step, scrub and step
// *backwards* without a request per step, and what lets an SDK caller assert on
// a run from a single call. A stateful stepping session would also have to hold
// the run's database transaction open between requests, which is the one thing
// this design is careful not to do.
type SimulationService interface {
	// Simulate runs one case to completion, to the first question it cannot
	// answer, or to its step budget.
	//
	// A run that ends in an incident is a *result*, not an error: the engine
	// refusing a decision point is the simulation doing its job, and it is the
	// finding the caller came for. Only a failure to run at all returns err.
	Simulate(ctx context.Context, req entities.SimulationRequest) (entities.SimulationRun, error)
}
