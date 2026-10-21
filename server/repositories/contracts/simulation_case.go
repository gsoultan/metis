package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// SimulationCaseRepository keeps the simulation test cases of each process.
// Every method is scoped to the caller's organization: a case of somebody
// else's project is answered as one that is not there.
type SimulationCaseRepository interface {
	// ListByProcess lists one process's cases, oldest first.
	ListByProcess(ctx context.Context, projectID uuid.UUID, processKey string) ([]entities.SimulationCase, error)
	// Create records a new case and returns it as stored.
	Create(ctx context.Context, c entities.SimulationCase) (entities.SimulationCase, error)
	// Update rewrites a case's name, scenario and expectation. It names the
	// project and process the case is under, and a case under any other is not
	// found: a case is never moved.
	Update(ctx context.Context, c entities.SimulationCase) (entities.SimulationCase, error)
	// Delete removes a case.
	Delete(ctx context.Context, id uuid.UUID) error
}
