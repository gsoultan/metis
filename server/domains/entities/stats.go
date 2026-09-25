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
