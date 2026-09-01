package external_task

import (
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
