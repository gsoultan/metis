package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// JobEnqueuer handles enqueueing different job types.
type JobEnqueuer interface {
	// EnqueueServiceTask queues the work for one service task. iterationID
	// names the iteration on a node that runs once per item, and is empty
	// otherwise — the job needs it to know which token it retires when it
	// finishes.
	EnqueueServiceTask(ctx context.Context, instance entities.ProcessInstance, node entities.Node, iterationID string) error
	EnqueueTimer(ctx context.Context, instance entities.ProcessInstance, node entities.Node, duration string) error
	// EnqueueBoundaryTimer enqueues a non-interrupting or interrupting timer boundary event
	// that fires after the given duration and triggers the boundary event on the attached task.
	EnqueueBoundaryTimer(ctx context.Context, instance entities.ProcessInstance, boundaryNode entities.Node, duration string) error
}

// JobWorker manages the job execution lifecycle.
type JobWorker interface {
	StartWorkers(ctx context.Context)

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

// JobService composes all job-related operations.
type JobService interface {
	JobEnqueuer
	JobWorker
	IncidentManager
}
