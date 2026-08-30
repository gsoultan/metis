package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// SagaActivityRecorder tracks completed activities that may need compensation.
// Call RecordActivity after each forward step completes successfully.
type SagaActivityRecorder interface {
	RecordActivity(ctx context.Context, activity entities.CompensatableActivity) error
}

// SagaCompensator triggers reverse execution of all recorded activities for an
// instance in reverse-completion order (last completed → first compensated).
type SagaCompensator interface {
	Compensate(ctx context.Context, instanceID uuid.UUID) error
}

// SagaCoordinator composes recording and compensation into the full Saga contract.
// Inject this into service tasks and the engine to enable automatic rollback of
// distributed transactions when a downstream step fails.
type SagaCoordinator interface {
	SagaActivityRecorder
	SagaCompensator
}
