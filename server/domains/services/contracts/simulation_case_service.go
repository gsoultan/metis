package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// SimulationCaseService keeps the simulation test cases a process is checked
// against: what each starts with, how it is answered, and what it must end
// with. Gated as simulation is, to whoever may design processes.
type SimulationCaseService interface {
	// ListSimulationCases lists one process's cases, oldest first.
	ListSimulationCases(ctx context.Context, projectID uuid.UUID, processKey string) ([]entities.SimulationCase, error)
	// SaveSimulationCase creates a case when it has no id, and rewrites the
	// case with that id otherwise.
	SaveSimulationCase(ctx context.Context, c entities.SimulationCase) (entities.SimulationCase, error)
	// DeleteSimulationCase removes a case.
	DeleteSimulationCase(ctx context.Context, id uuid.UUID) error
}
