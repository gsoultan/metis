package entities

import (
	"time"

	"github.com/google/uuid"
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
	JobServiceTask   JobType = "service_task"
	JobTimer         JobType = "timer"
	JobTimerBoundary JobType = "timer_boundary"
)

type Job struct {
	ID         uuid.UUID          `json:"id"`
	Instance   *ProcessInstance   `json:"instance,omitzero"`
	Definition *ProcessDefinition `json:"definition,omitzero"`
	Node       *Node              `json:"node,omitzero"`

	// IterationID names the iteration this job is for, on a node that runs once
	// per item. Empty for an ordinary node.
	//
	// Without it, finishing the job could not say which of the node's tokens to
	// retire: a service task told to run once per supplier ran for every one and
	// then left every token in place, so the process sat there looking busy with
	// nothing left to do.
	IterationID string         `json:"iteration_id,omitzero"`
	Type        JobType        `json:"type"`
	Status      JobStatus      `json:"status"`
	Payload     map[string]any `json:"payload"`
	Retries     int            `json:"retries"`
	MaxRetries  int            `json:"maxRetries"`
	NextRunAt   time.Time      `json:"next_run_at"`

	// RepeatsRemaining is how many further occurrences a repeating timer
	// (BPMN timeCycle, "R3/PT10M") still owes after this one. Zero means this is
	// the last, RepeatsForever means unbounded. Only timer jobs use it.
	RepeatsRemaining int       `json:"repeats_remaining,omitzero"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	LastError        string    `json:"last_error,omitzero"`
}
