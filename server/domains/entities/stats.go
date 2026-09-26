package entities

import (
	"time"

	"github.com/google/uuid"
)

// ProcessStatistics represents high-level metrics for a project or the system.
type ProcessStatistics struct {
	ActiveInstances    int `json:"active_instances"`
	CompletedInstances int `json:"completed_instances"`
	FailedInstances    int `json:"failed_instances"`
	TotalTasks         int `json:"total_tasks"`
	PendingTasks       int `json:"pending_tasks"`
	// CompletedTasks is what the dashboard's completion rate is of. The rate
	// was total minus unclaimed, so a task somebody had only claimed counted
	// as done.
	CompletedTasks int `json:"completed_tasks"`
}

// WaitingProcess is where one process's running work is sitting right now.
type WaitingProcess struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Instances counts the running instances sitting on at least one step. An
	// instance on two parallel branches is one instance, waiting in two places.
	Instances int           `json:"instances"`
	Steps     []WaitingStep `json:"steps"`
}

// WaitingStep is one step and how much work is sitting on it.
type WaitingStep struct {
	NodeID  string `json:"node_id"`
	Waiting int    `json:"waiting"`
}

// Deadlines is a project's open work as far as deadlines go.
type Deadlines struct {
	// Tasks are the open tasks with a due date, soonest first, at most as many
	// as the server sends. The overdue ones come before any that are not.
	Tasks []DeadlineTask `json:"tasks"`
	// WithDeadline and WithoutDeadline count all of the project's open tasks,
	// not only the ones sent.
	WithDeadline    int `json:"with_deadline"`
	WithoutDeadline int `json:"without_deadline"`
}

// DeadlineTask is one open task with a due date, and the process it is part of.
type DeadlineTask struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	NodeID      string    `json:"node_id"`
	Status      string    `json:"status"`
	Priority    int       `json:"priority"`
	Assignee    string    `json:"assignee,omitzero"`
	DueDate     time.Time `json:"due_date"`
	ProcessKey  string    `json:"process_key"`
	ProcessName string    `json:"process_name"`
}
