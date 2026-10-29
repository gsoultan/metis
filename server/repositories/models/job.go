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
	InstanceID       UUID           `json:"instance_id,omitzero"`
	DefinitionID     UUID           `json:"definition_id,omitzero"`
	OrganizationID   *UUID          `json:"organization_id,omitzero"`
	NodeID           string         `json:"node_id"`
	IterationID      string         `json:"iteration_id,omitzero"`
	Type             JobType        `json:"type"`
	Status           JobStatus      `json:"status"`
	LockedBy         string         `json:"locked_by,omitzero"`
	LockExpires      *time.Time     `json:"lock_expires,omitzero"`
	Payload          map[string]any `json:"payload,omitzero"`
	Retries          int            `json:"retries"`
	MaxRetries       int            `json:"max_retries"`
	RepeatsRemaining int            `json:"repeats_remaining,omitzero"`
	NextRunAt        time.Time      `json:"next_run_at,omitzero"`
	LastError        string         `json:"last_error,omitzero"`
}

func (JobModel) TableName() string {
	return "jobs"
}
