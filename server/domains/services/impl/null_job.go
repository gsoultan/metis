package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
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

// ProcessPendingJobs has nothing to process: this service never enqueues.
func (s *NullJobService) ProcessPendingJobs(_ context.Context) error { return nil }

func (s *NullJobService) ListIncidents(_ context.Context, _ uuid.UUID) ([]entities.Incident, error) {
	return nil, nil
}

func (s *NullJobService) ResolveIncident(_ context.Context, _ uuid.UUID) error {
	return nil
}

func NewNullJobService() contracts.JobService {
	return &NullJobService{}
}
