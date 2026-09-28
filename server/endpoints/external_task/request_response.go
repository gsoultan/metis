package external_task

import (
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type FetchAndLockExternalRequest struct {
	Topic        string
	WorkerID     string
	MaxTasks     int
	LockDuration int64
}

type FetchAndLockExternalResponse struct {
	Tasks []*entities.ExternalTask `json:"tasks"`
	Error string                   `json:"error,omitempty"`
}

type CompleteExternalRequest struct {
	TaskID    uuid.UUID
	WorkerID  string
	Variables map[string]any
}

type CompleteExternalResponse struct {
	Error string `json:"error,omitempty"`
}

type HandleExternalFailureRequest struct {
	TaskID       uuid.UUID
	WorkerID     string
	ErrorMessage string
	ErrorDetails string
	Retries      int
	RetryTimeout int64
}

type HandleExternalFailureResponse struct {
	Error string `json:"error,omitempty"`
}

// ExtendExternalLockRequest asks for more time on a lock the worker holds.
type ExtendExternalLockRequest struct {
	TaskID   uuid.UUID
	WorkerID string
	// LockDuration is how long from now the lock is to run, in milliseconds.
	LockDuration int64
}

// ExtendExternalLockResponse is when the lock now runs out.
//
// A Failer, unlike its neighbours, which answer a refusal over HTTP as a 200
// with the reason in a field. A worker that does not read the field takes
// that for success, and a worker told it still holds a task it has lost is
// the double execution the lock exists to prevent.
type ExtendExternalLockResponse struct {
	LockExpiration time.Time `json:"lock_expiration"`
	Err            error     `json:"err,omitzero"`
}

func (r ExtendExternalLockResponse) Failed() error { return r.Err }
