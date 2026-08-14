package models

import (
	"time"
)

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

type JobType string

const (
	JobServiceTask JobType = "service_task"
	JobTimer       JobType = "timer"
)

type JobModel struct {
	Base
	InstanceID   UUID           `gorm:"index" json:"instance_id,omitzero"`
	DefinitionID UUID           `json:"definition_id,omitzero"`
	NodeID       string         `json:"node_id"`
	IterationID  string         `json:"iteration_id,omitzero"`
	Type         JobType        `json:"type"`
	Status       JobStatus      `gorm:"index" json:"status"`
	LockedBy     string         `json:"locked_by,omitzero"`
	LockExpires  *time.Time     `json:"lock_expires,omitzero"`
	Payload      map[string]any `gorm:"type:text;serializer:json" json:"payload,omitzero"`
	Retries      int            `json:"retries"`
	MaxRetries   int            `json:"max_retries"`
	NextRunAt    time.Time      `gorm:"index" json:"next_run_at,omitzero"`
	LastError    string         `json:"last_error,omitzero"`
}

func (JobModel) TableName() string {
	return "jobs"
}
