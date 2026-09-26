package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
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
	//
	// call.JobID names the visit: a step visited twice, through a loop, is two
	// jobs and two calls, while a retry of one visit is the same job. A row
	// written before the visit was part of the identity carries no job; it is
	// taken as this visit's only if it was written after visitStarted — the
	// job's creation — which makes it an earlier attempt of this visit rather
	// than a loop's previous pass.
	Begin(ctx context.Context, call models.ServiceCallModel, visitStarted time.Time) (models.ServiceCallModel, error)

	// Complete records the response and closes the call.
	Complete(ctx context.Context, id uuid.UUID, response map[string]any) error

	// Get returns the most recent call recorded for one step of one instance.
	Get(ctx context.Context, instanceID uuid.UUID, nodeID, iterationID string) (models.ServiceCallModel, error)
}
