package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

type NullJobService struct{}

func (s *NullJobService) EnqueueServiceTask(_ context.Context, _ entities.ProcessInstance, _ entities.Node, _ string) error {
	return nil
}

func (s *NullJobService) EnqueueTimer(_ context.Context, _ entities.ProcessInstance, _ entities.Node, _ string) error {
	return nil
}

func (s *NullJobService) EnqueueBoundaryTimer(_ context.Context, _ entities.ProcessInstance, _ entities.Node, _ string) error {
	return nil
}

func (s *NullJobService) StartWorkers(_ context.Context) {}

// StopWorkers has nothing in flight: this service never started a worker.
func (s *NullJobService) StopWorkers(_ context.Context) error { return nil }

// ProcessPendingJobs has nothing to process: this service never enqueues.
func (s *NullJobService) ProcessPendingJobs(_ context.Context) error { return nil }

func (s *NullJobService) ListIncidents(_ context.Context, _ uuid.UUID) ([]entities.Incident, error) {
	return nil, nil
}

func (s *NullJobService) ResolveIncident(_ context.Context, _ uuid.UUID) error {
	return nil
}

// TryConnectorStep refuses rather than answering with nothing: an empty result
// would read, in the designer, as a step that ran and returned nothing.
func (s *NullJobService) TryConnectorStep(_ context.Context, _ uuid.UUID, _ entities.Node, _ map[string]any) (map[string]any, error) {
	return nil, errNoJobService
}

// errNoJobService is what a step tried with no job service behind it hears.
var errNoJobService = errors.New("no job service is running here, so a step cannot be tried")

func NewNullJobService() contracts.JobService {
	return &NullJobService{}
}
