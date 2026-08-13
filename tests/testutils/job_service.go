package testutils

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	contracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
)

type SynchronousJobService struct {
	engine contracts.ExecutionEngine
	repo   repositories.Repository
}

func NewSynchronousJobService(engine contracts.ExecutionEngine, repo repositories.Repository) contracts.JobService {
	return &SynchronousJobService{engine: engine, repo: repo}
}

func (s *SynchronousJobService) EnqueueServiceTask(ctx context.Context, instance entities.ProcessInstance, node entities.Node, _ string) error {
	// Simulate what the job worker would do
	if instance.Variables == nil {
		instance.Variables = make(map[string]any)
	}
	instance.Variables[node.ID+"_completed"] = true

	// UpdateConnectorInstance instance in repo
	if err := s.engine.UpdateInstance(ctx, instance); err != nil {
		return err
	}

	def, err := s.engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return err
	}

	// Execute immediately for testing
	return s.engine.Proceed(ctx, &instance, def, node.ID)
}

func (s *SynchronousJobService) EnqueueTimer(ctx context.Context, instance entities.ProcessInstance, node entities.Node, duration string) error {
	if d, err := time.ParseDuration(duration); err == nil {
		time.Sleep(d)
	}

	def, err := s.engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return err
	}

	// Execute immediately for testing
	return s.engine.Proceed(ctx, &instance, def, node.ID)
}

func (s *SynchronousJobService) EnqueueBoundaryTimer(ctx context.Context, instance entities.ProcessInstance, boundaryNode entities.Node, _ string) error {
	def, err := s.engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return err
	}
	return s.engine.ExecuteNode(ctx, &instance, def, boundaryNode.ID)
}

func (s *SynchronousJobService) StartWorkers(_ context.Context) {}

// ProcessPendingJobs has nothing to process: this double runs each task inline
// at enqueue time rather than persisting a job. Use the real job service when
// what a service task actually does is the thing under test.
func (s *SynchronousJobService) ProcessPendingJobs(_ context.Context) error { return nil }

func (s *SynchronousJobService) ListIncidents(ctx context.Context, instanceID uuid.UUID) ([]entities.Incident, error) {
	return nil, nil
}

func (s *SynchronousJobService) ResolveIncident(ctx context.Context, incidentID uuid.UUID) error {
	return nil
}
