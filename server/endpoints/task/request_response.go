package task

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

type GetTaskRequest struct {
	ID string `json:"id"`
}

type GetTaskResponse struct {
	Task entities.Task `json:"task,omitzero"`
	Err  error         `json:"err,omitzero"`
}

func (r GetTaskResponse) Failed() error { return r.Err }

type ListTasksRequest struct {
	// Zero means "no paging requested" — the first page at the server default.
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`

	ProjectID string `json:"project_id,omitzero"`

	// InstanceID narrows the listing to one process instance. It takes
	// precedence over ProjectID, which it already implies — an instance belongs
	// to exactly one project.
	InstanceID string `json:"instance_id,omitzero"`
}

type ListTasksResponse struct {
	Page  *PageInfo       `json:"page,omitempty"`
	Tasks []entities.Task `json:"tasks,omitzero"`
	Err   error           `json:"err,omitzero"`
}

func (r ListTasksResponse) Failed() error { return r.Err }

// PageInfo describes the window returned, when the caller paged. Nil otherwise,
// so an unpaged response is byte-identical to what it was before.
type PageInfo struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}

type ListTasksByAssigneeRequest struct {
	Assignee string `json:"assignee"`
	// Zero means "no paging requested", which returns the first page at the
	// server default. Clients written before paging existed keep working.
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

type ListTasksByCandidatesRequest struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`

	UserID string   `json:"user_id"`
	Groups []string `json:"groups"`
}

type ClaimTaskRequest struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
}

type UnclaimTaskRequest struct {
	ID string `json:"id"`
	// Reason is why the task is released. Required unless the caller holds it.
	Reason string `json:"reason,omitzero"`
}

type DelegateTaskRequest struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	// Reason is why the task is delegated. Required unless the caller holds it.
	Reason string `json:"reason,omitzero"`
}

type CompleteTaskRequest struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Variables map[string]any `json:"variables,omitzero"`
}

type CompleteTaskResponse struct {
	Err error `json:"err,omitzero"`
}

func (r CompleteTaskResponse) Failed() error { return r.Err }

// UpdateTaskRequest changes a task's name, priority or due date. Only the
// fields the request carries change: a field left out is left as it is.
type UpdateTaskRequest struct {
	ID       string  `json:"id"`
	Name     *string `json:"name,omitempty"`
	Priority *int    `json:"priority,omitempty"`
	// DueDate is absent to keep the due date, null or "" to remove it, or an
	// RFC 3339 time to set it.
	DueDate DueDateField `json:"due_date"`
	// Reason is why the task is changed. Required unless the caller holds it.
	Reason string `json:"reason,omitzero"`
}

type UpdateTaskResponse struct {
	Err error `json:"err,omitzero"`
}

func (r UpdateTaskResponse) Failed() error { return r.Err }

type AssignTaskRequest struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	// Reason is why the task is assigned. Required unless the caller holds it.
	Reason string `json:"reason,omitzero"`
}

type AssignTaskResponse struct {
	Err error `json:"err,omitzero"`
}

func (r AssignTaskResponse) Failed() error { return r.Err }

// ResolveTaskRequest hands a delegated task back to its owner.
type ResolveTaskRequest struct {
	ID string `json:"id"`
	// Reason is why. Required unless the caller is the delegate holding it.
	Reason string `json:"reason,omitzero"`
}

type ResolveTaskResponse struct {
	Err error `json:"err,omitzero"`
}

func (r ResolveTaskResponse) Failed() error { return r.Err }

// ListDelegatedTasksRequest asks for the tasks the caller delegated that are
// still with their delegate. It names nobody: the caller is who the token says.
type ListDelegatedTasksRequest struct {
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`
}
