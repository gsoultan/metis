package entities

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
