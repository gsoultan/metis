package batchoperation

import (
	"net/http"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
)

// FilterView chooses the instances an operation acts on: the instance list's
// filter, every field optional.
type FilterView struct {
	Status         string `json:"status,omitzero"`
	DefinitionID   string `json:"definition_id,omitzero"`
	BusinessKey    string `json:"business_key,omitzero"`
	NeedsAttention bool   `json:"needs_attention,omitzero"`
}

// StartRequest asks for an operation on a project's instances. It is a dry
// run unless dry_run is false: a dry run says how many instances the filter
// matches and changes nothing.
type StartRequest struct {
	// ProjectID is taken from the address, never from the body.
	ProjectID string     `json:"-"`
	Kind      string     `json:"kind"`
	Reason    string     `json:"reason"`
	Filter    FilterView `json:"filter"`
	DryRun    *bool      `json:"dry_run"`
}

// StartResponse is the preview and, when the operation was started, the
// operation.
type StartResponse struct {
	Kind      string         `json:"kind"`
	Matched   int64          `json:"matched"`
	TooMany   bool           `json:"too_many"`
	Limit     int            `json:"limit"`
	Operation *OperationView `json:"operation,omitzero"`
	Err       error          `json:"err,omitzero"`
}

func (r StartResponse) Failed() error { return r.Err }

// StatusCode is 202 for an operation that was started — it is worked through
// after the reply — and 200 for a preview.
func (r StartResponse) StatusCode() int {
	if r.Operation != nil {
		return http.StatusAccepted
	}
	return http.StatusOK
}

// OperationRequest names one operation.
type OperationRequest struct {
	ID string `json:"-"`
}

// OperationResponse is one operation.
type OperationResponse struct {
	Operation *OperationView `json:"operation,omitzero"`
	Err       error          `json:"err,omitzero"`
}

func (r OperationResponse) Failed() error { return r.Err }

// StopRequest stops an operation, with the reason kept beside who stopped it.
type StopRequest struct {
	ID     string `json:"-"`
	Reason string `json:"reason"`
}

// ListRequest names the project whose operations are listed.
type ListRequest struct {
	ProjectID string `json:"-"`
}

// ListResponse is a project's operations, newest first.
type ListResponse struct {
	Operations []OperationView `json:"operations"`
	Err        error           `json:"err,omitzero"`
}

func (r ListResponse) Failed() error { return r.Err }

// ItemsRequest reads a page of what came of an operation's instances.
type ItemsRequest struct {
	ID       string `json:"-"`
	Status   string `json:"-"`
	Page     int    `json:"-"`
	PageSize int    `json:"-"`
}

// ItemsResponse is a page of an operation's instances.
type ItemsResponse struct {
	Items    []ItemView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
	Err      error      `json:"err,omitzero"`
}

func (r ItemsResponse) Failed() error { return r.Err }

// OperationView is an operation as the API shows it.
type OperationView struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	Kind        string     `json:"kind"`
	Reason      string     `json:"reason"`
	Filter      FilterView `json:"filter"`
	RequestedBy string     `json:"requested_by"`
	RequestedAt time.Time  `json:"requested_at"`
	Status      string     `json:"status"`
	Total       int64      `json:"total"`
	Succeeded   int64      `json:"succeeded"`
	Failed      int64      `json:"failed"`
	Skipped     int64      `json:"skipped"`
	Pending     int64      `json:"pending"`
	FinishedAt  *time.Time `json:"finished_at,omitzero"`
	StoppedBy   string     `json:"stopped_by,omitzero"`
	StopReason  string     `json:"stop_reason,omitzero"`
}

// ItemView is one instance of an operation and what came of it.
type ItemView struct {
	InstanceID string     `json:"instance_id"`
	Status     string     `json:"status"`
	Detail     string     `json:"detail,omitzero"`
	SettledAt  *time.Time `json:"settled_at,omitzero"`
}

// ViewOf maps an operation to its view.
func ViewOf(operation entities.BatchOperation) OperationView {
	view := OperationView{
		ID:        operation.ID.String(),
		ProjectID: operation.ProjectID.String(),
		Kind:      string(operation.Kind),
		Reason:    operation.Reason,
		Filter: FilterView{
			Status:         operation.Filter.Status,
			BusinessKey:    operation.Filter.BusinessKey,
			NeedsAttention: operation.Filter.NeedsAttention,
		},
		RequestedBy: operation.RequestedBy,
		RequestedAt: operation.CreatedAt,
		Status:      string(operation.Status),
		Total:       operation.Total,
		Succeeded:   operation.Succeeded,
		Failed:      operation.Failed,
		Skipped:     operation.Skipped,
		Pending:     operation.Pending(),
		FinishedAt:  operation.FinishedAt,
		StoppedBy:   operation.StoppedBy,
		StopReason:  operation.StopReason,
	}
	if operation.Filter.DefinitionID != [16]byte{} {
		view.Filter.DefinitionID = operation.Filter.DefinitionID.String()
	}
	return view
}

// ItemsOf maps a page of items to the reply.
func ItemsOf(page contracts.Page[entities.BatchItem]) ItemsResponse {
	items := make([]ItemView, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, ItemView{
			InstanceID: item.InstanceID.String(),
			Status:     string(item.Status),
			Detail:     item.Detail,
			SettledAt:  item.SettledAt,
		})
	}
	return ItemsResponse{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}
}
