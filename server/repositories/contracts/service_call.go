package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// ServiceCallRepository remembers outbound calls across job attempts.
//
// A service task's call cannot share a transaction with the token advance that
// follows it — the call is network I/O and the advance takes a row lock — so a
// commit failure after a successful call means the job is retried and the call
// is made again. This repository is the memory that makes the second attempt
// know about the first.
type ServiceCallRepository interface {
	// Begin claims a call, returning what is already known about it.
	//
	// The first attempt records the call in flight and reports started=true. A
	// retry finds the existing row: completed, and the caller reuses the stored
	// response instead of calling again; still in flight, and the caller repeats
	// the call carrying the same idempotency key, which is the only safe move
	// when a client cannot tell a request that never arrived from one whose
	// response was lost.
	Begin(ctx context.Context, call models.ServiceCallModel) (models.ServiceCallModel, error)

	// Complete records the response and closes the call.
	Complete(ctx context.Context, id uuid.UUID, response map[string]any) error

	// Get returns a recorded call by its identity.
	Get(ctx context.Context, instanceID uuid.UUID, nodeID, iterationID string) (models.ServiceCallModel, error)
}
