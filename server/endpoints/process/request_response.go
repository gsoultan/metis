package process

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type StartProcessRequest struct {
	ProjectID     string         `json:"project_id"`
	DefinitionKey string         `json:"definition_key"`
	Variables     map[string]any `json:"variables,omitzero"`

	// Version starts a named version instead of the live one. Zero means the
	// live one, which is what a caller who does not care about versions gets —
	// and what every caller written before this field existed sends.
	//
	// The use it exists for is trying a staged version: deploy without promoting,
	// start one instance on it deliberately, then promote once it behaves.
	// Without this the only way to exercise a new version was to make it live for
	// everybody first.
	Version int `json:"version,omitzero"`
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
	// Status narrows to one lifecycle state across the whole project. Empty
	// means every state; anything not in models.ProcessStatuses is refused.
	Status string `json:"status,omitzero"`
	// DefinitionID narrows to runs of one process. Empty means every process.
	DefinitionID string `json:"definition_id,omitzero"`
	// NeedsAttention narrows to instances holding an unresolved incident.
	NeedsAttention bool `json:"needs_attention,omitzero"`
}

// InstanceStatusCount is how many of a project's instances are in one state.
type InstanceStatusCount struct {
	Status string `json:"status"`
	Total  int64  `json:"total"`
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
	// StatusCounts describes the whole project, not this page — so a caller
	// showing twenty-five completed runs can still say twelve need attention,
	// and offer the filter that reaches them.
	StatusCounts []InstanceStatusCount `json:"status_counts,omitempty"`
	// NeedsAttentionTotal is how many of the project's instances hold an
	// unresolved incident. Separate from StatusCounts because it is not a
	// status: the engine leaves such an instance `active`.
	NeedsAttentionTotal int64 `json:"needs_attention_total,omitzero"`
	// NeedsAttentionIDs are the instances on this page that hold one.
	NeedsAttentionIDs []string `json:"needs_attention_ids,omitempty"`
	Err               error    `json:"err,omitzero"`
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

type ExportOCELRequest struct {
	ProjectID string `json:"project_id"`
	// IncludeVariables is an explicit opt-in, and its absence must mean off.
	// The audit trail's data is the instance's process variables, so a default
	// of "include" would make this endpoint a way to take every business fact
	// in a project out in one request.
	IncludeVariables bool `json:"include_variables"`
}

type ExportOCELResponse struct {
	Log entities.OCELLog `json:"log,omitzero"`
	Err error            `json:"err,omitzero"`
}

func (r ExportOCELResponse) Failed() error { return r.Err }

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

// ActivateAdHocTaskRequest names one step inside an ad-hoc sub-process to start.
type ActivateAdHocTaskRequest struct {
	InstanceID       string `json:"instance_id"`
	SubProcessNodeID string `json:"sub_process_node_id"`
	TaskNodeID       string `json:"task_node_id"`
}

type ActivateAdHocTaskResponse struct {
	Err error `json:"err,omitzero"`
}

func (r ActivateAdHocTaskResponse) Failed() error { return r.Err }

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
