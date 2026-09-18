package models

import (
	"time"
)

// ExternalTaskModel is the database model for external tasks.
type ExternalTaskModel struct {
	Base
	ProjectID UUID `gorm:"index" json:"project_id,omitzero"`
	// The columns are instance_id and definition_id, matching every other
	// table that references them. This model spelled them out in full and was
	// the only one that did.
	ProcessInstanceID   UUID           `gorm:"column:instance_id;index" json:"process_instance_id,omitzero"`
	ProcessDefinitionID UUID           `gorm:"column:definition_id;index" json:"process_definition_id,omitzero"`
	NodeID              string         `gorm:"type:varchar(255)" json:"node_id"`
	Topic               string         `gorm:"index;type:varchar(255)" json:"topic"`
	WorkerID            string         `gorm:"index;type:varchar(255)" json:"worker_id,omitzero"`
	LockExpiration      *time.Time     `gorm:"index" json:"lock_expiration,omitzero"`
	Retries             int            `gorm:"not null;default:0" json:"retries"`
	RetryTimeout        int64          `gorm:"not null;default:0" json:"retry_timeout"`
	ErrorMessage        string         `gorm:"type:text" json:"error_message,omitzero"`
	ErrorDetails        string         `gorm:"type:text" json:"error_details,omitzero"`
	Variables           map[string]any `gorm:"type:text;serializer:json" json:"variables,omitzero"`
}

func (ExternalTaskModel) TableName() string {
	return "external_tasks"
}
