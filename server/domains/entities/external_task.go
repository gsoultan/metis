package entities

import (
	"time"

	"github.com/google/uuid"
)

// ExternalTask represents a task that is completed by an external worker.
type ExternalTask struct {
	ID                uuid.UUID          `json:"id"`
	Project           *Project           `json:"project,omitzero"`
	ProcessInstance   *ProcessInstance   `json:"process_instance,omitzero"`
	ProcessDefinition *ProcessDefinition `json:"process_definition,omitzero"`
	Node              *Node              `json:"node,omitzero"`
	Topic             string             `json:"topic"`
	WorkerID          string             `json:"worker_id,omitzero"`
	LockExpiration    *time.Time         `json:"lock_expiration,omitzero"`
	Retries           int                `json:"retries,omitzero"`
	RetryTimeout      int64              `json:"retry_timeout,omitzero"`
	ErrorMessage      string             `json:"error_message,omitzero"`
	ErrorDetails      string             `json:"error_details,omitzero"`
	Variables         map[string]any     `json:"variables,omitzero"`
	CreatedAt         time.Time          `json:"created_at,omitzero"`
}
