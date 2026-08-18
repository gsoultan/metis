package decision

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type ListDecisionsRequest struct {
	ProjectID string `json:"project_id,omitzero"`

	// Zero means "no paging requested" — the first page at the server default.
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`
}

type ListDecisionsResponse struct {
	// Page describes the window returned, so a caller can say "1–50 of 340".
	Page      *PageInfo                     `json:"page,omitempty"`
	Decisions []entities.DecisionDefinition `json:"decisions,omitzero"`
	Err       error                         `json:"err,omitzero"`
}

// PageInfo describes the window returned.
type PageInfo struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}

func (r ListDecisionsResponse) Failed() error { return r.Err }

type GetDecisionRequest struct {
	ID string `json:"id"`
}

type GetDecisionResponse struct {
	Decision entities.DecisionDefinition `json:"decision,omitzero"`
	Err      error                       `json:"err,omitzero"`
}

func (r GetDecisionResponse) Failed() error { return r.Err }

type CreateDecisionRequest struct {
	Decision entities.DecisionDefinition `json:"decision,omitzero"`
}

type CreateDecisionResponse struct {
	ID  uuid.UUID `json:"id"`
	Err error     `json:"err,omitzero"`
}

func (r CreateDecisionResponse) Failed() error { return r.Err }

type UpdateDecisionRequest struct {
	ID       string                      `json:"id"`
	Decision entities.DecisionDefinition `json:"decision,omitzero"`
}

type UpdateDecisionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r UpdateDecisionResponse) Failed() error { return r.Err }

type DeleteDecisionRequest struct {
	ID string `json:"id"`
}

type DeleteDecisionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteDecisionResponse) Failed() error { return r.Err }

type EvaluateDecisionRequest struct {
	Key       string         `json:"key"`
	Version   int            `json:"version,omitzero"`
	Variables map[string]any `json:"variables,omitzero"`
}

type EvaluateDecisionResponse struct {
	Result entities.DecisionResult `json:"result,omitzero"`
	Err    error                   `json:"err,omitzero"`
}

func (r EvaluateDecisionResponse) Failed() error { return r.Err }

// DecisionImpactRequest asks what depends on a decision.
type DecisionImpactRequest struct {
	ID string `json:"id"`
}

// DecisionImpactResponse carries the processes that consult it.
type DecisionImpactResponse struct {
	Impact entities.DecisionImpact `json:"impact,omitzero"`
	Err    error                   `json:"err,omitzero"`
}

func (r DecisionImpactResponse) Failed() error { return r.Err }

// RunDecisionTestsRequest runs a table against its stored examples.
type RunDecisionTestsRequest struct {
	ID string `json:"id"`
}

// RunDecisionTestsResponse carries one result per example.
type RunDecisionTestsResponse struct {
	Results []entities.DecisionTestResult `json:"results,omitzero"`
	Err     error                         `json:"err,omitzero"`
}

func (r RunDecisionTestsResponse) Failed() error { return r.Err }
