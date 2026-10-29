package models

import (
	"time"
)

// ExternalTaskModel is the database model for external tasks.
type ExternalTaskModel struct {
	Base
	ProjectID UUID `json:"project_id,omitzero"`
	// The columns are instance_id and definition_id, matching every other
	// table that references them. This model spelled them out in full and was
	// the only one that did.
	ProcessInstanceID   UUID           `json:"process_instance_id,omitzero"`
	ProcessDefinitionID UUID           `json:"process_definition_id,omitzero"`
	NodeID              string         `json:"node_id"`
	Topic               string         `json:"topic"`
	WorkerID            string         `json:"worker_id,omitzero"`
	LockExpiration      *time.Time     `json:"lock_expiration,omitzero"`
	Retries             int            `json:"retries"`
	RetryTimeout        int64          `json:"retry_timeout"`
	ErrorMessage        string         `json:"error_message,omitzero"`
	ErrorDetails        string         `json:"error_details,omitzero"`
	Variables           map[string]any `json:"variables,omitzero"`
}

func (ExternalTaskModel) TableName() string {
	return "external_tasks"
}
