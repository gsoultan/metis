package task

import (
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"
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
	ProjectID string `json:"project_id,omitzero"`
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
	UserID string   `json:"user_id"`
	Groups []string `json:"groups"`
}

type ClaimTaskRequest struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
}

type UnclaimTaskRequest struct {
	ID string `json:"id"`
}

type DelegateTaskRequest struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
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

type UpdateTaskRequest struct {
	ID       string     `json:"id"`
	Name     string     `json:"name,omitzero"`
	Priority int        `json:"priority,omitzero"`
	DueDate  *time.Time `json:"due_date,omitzero"`
}

type UpdateTaskResponse struct {
	Err error `json:"err,omitzero"`
}

func (r UpdateTaskResponse) Failed() error { return r.Err }

type AssignTaskRequest struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
}

type AssignTaskResponse struct {
	Err error `json:"err,omitzero"`
}

func (r AssignTaskResponse) Failed() error { return r.Err }
