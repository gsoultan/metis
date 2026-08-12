package process

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type StartProcessRequest struct {
	ProjectID     string         `json:"project_id"`
	DefinitionKey string         `json:"definition_key"`
	Variables     map[string]any `json:"variables,omitzero"`
}

type StartProcessResponse struct {
	InstanceID uuid.UUID `json:"instance_id"`
	Err        error     `json:"err,omitzero"`
}

func (r StartProcessResponse) Failed() error { return r.Err }

type ListInstancesRequest struct {
	ProjectID string `json:"project_id,omitzero"`
	// Zero means "no paging requested" — the first page at the server default.
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`
}

// InstancePageInfo describes the window returned, when the caller paged.
type InstancePageInfo struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}

type ListInstancesResponse struct {
	Instances []entities.ProcessInstance `json:"instances"`
	Page      *InstancePageInfo          `json:"page,omitempty"`
	Err       error                      `json:"err,omitzero"`
}

func (r ListInstancesResponse) Failed() error { return r.Err }

type GetExecutionPathRequest struct {
	InstanceID string `json:"instance_id"`
}

type GetExecutionPathResponse struct {
	Nodes       []*entities.Node `json:"nodes"`
	Frequencies map[string]int   `json:"frequencies,omitzero"`
	Error       string           `json:"error,omitempty"`
}

type GetAuditLogsRequest struct {
	InstanceID string `json:"instance_id"`
}

type GetAuditLogsResponse struct {
	Entries []entities.AuditEntry `json:"entries"`
	Err     error                 `json:"err,omitzero"`
}

func (r GetAuditLogsResponse) Failed() error { return r.Err }

type GetInstanceRequest struct {
	ID string `json:"id"`
}

type GetInstanceResponse struct {
	Instance entities.ProcessInstance `json:"instance,omitzero"`
	Err      error                    `json:"err,omitzero"`
}

func (r GetInstanceResponse) Failed() error { return r.Err }

type ListSubProcessesRequest struct {
	ParentInstanceID string `json:"parent_instance_id"`
}

type ListSubProcessesResponse struct {
	Instances []entities.ProcessInstance `json:"instances"`
	Err       error                      `json:"err,omitzero"`
}

func (r ListSubProcessesResponse) Failed() error { return r.Err }

type GetProcessStatisticsRequest struct {
	ProjectID string `json:"project_id,omitzero"`
}

type GetProcessStatisticsResponse struct {
	ActiveInstances    int            `json:"active_instances"`
	CompletedInstances int            `json:"completed_instances"`
	FailedInstances    int            `json:"failed_instances"`
	TotalTasks         int            `json:"total_tasks"`
	PendingTasks       int            `json:"pending_tasks"`
	NodeFrequencies    map[string]int `json:"node_frequencies,omitzero"`
	Err                error          `json:"err,omitzero"`
}

func (r GetProcessStatisticsResponse) Failed() error { return r.Err }

type BroadcastSignalRequest struct {
	ProjectID  string         `json:"project_id"`
	SignalName string         `json:"signal_name"`
	Variables  map[string]any `json:"variables,omitzero"`
}

type BroadcastSignalResponse struct {
	Err error `json:"err,omitzero"`
}

func (r BroadcastSignalResponse) Failed() error { return r.Err }

type SendMessageRequest struct {
	ProjectID      string         `json:"project_id"`
	MessageName    string         `json:"message_name"`
	CorrelationKey string         `json:"correlation_key,omitzero"`
	Variables      map[string]any `json:"variables,omitzero"`
}

type SendMessageResponse struct {
	Err error `json:"err,omitzero"`
}

func (r SendMessageResponse) Failed() error { return r.Err }

type ExecuteScriptRequest struct {
	Script       string         `json:"script"`
	ScriptFormat string         `json:"script_format"`
	Variables    map[string]any `json:"variables,omitzero"`
}

type ExecuteScriptResponse struct {
	Variables map[string]any `json:"variables,omitzero"`
	Err       error          `json:"err,omitzero"`
}

func (r ExecuteScriptResponse) Failed() error { return r.Err }
