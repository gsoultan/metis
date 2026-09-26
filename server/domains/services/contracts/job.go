package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// JobEnqueuer handles enqueueing different job types.
type JobEnqueuer interface {
	// EnqueueServiceTask queues the work for one service task. iterationID
	// names the iteration on a node that runs once per item, and is empty
	// otherwise — the job needs it to know which token it retires when it
	// finishes.
	EnqueueServiceTask(ctx context.Context, instance entities.ProcessInstance, node entities.Node, iterationID string) error
	EnqueueTimer(ctx context.Context, instance entities.ProcessInstance, node entities.Node, duration string) error
}

// JobWorker manages the job execution lifecycle.
type JobWorker interface {
	StartWorkers(ctx context.Context)
	// StopWorkers stops claiming jobs and waits, briefly, for the ones already
	// claimed. Without it a shutdown abandoned in-flight work: the final status
	// write rode the cancelled context and failed, so the row kept its lock
	// until the lease expired and the job was frozen for minutes.
	StopWorkers(ctx context.Context) error

	// ProcessPendingJobs runs one round of pending jobs and waits for it to
	// finish, for a caller that needs the work done before it looks at the
	// result rather than eventually.
	ProcessPendingJobs(ctx context.Context) error
}

// IncidentManager handles job incidents.
type IncidentManager interface {
	ListIncidents(ctx context.Context, instanceID uuid.UUID) ([]entities.Incident, error)
	ResolveIncident(ctx context.Context, incidentID uuid.UUID) error
}

// ConnectorStepTrier runs one connector step once, outside any process.
type ConnectorStepTrier interface {
	// TryConnectorStep runs the step as the job would — against its project's
	// saved connection, with its mappings — and returns what the step would
	// store. No job, no service call record, no process touched.
	TryConnectorStep(ctx context.Context, projectID uuid.UUID, node entities.Node, variables map[string]any) (map[string]any, error)
}

// JobService composes all job-related operations.
type JobService interface {
	JobEnqueuer
	JobWorker
	IncidentManager
	ConnectorStepTrier
}
