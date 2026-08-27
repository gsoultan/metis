package definition

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type ListDefinitionsRequest struct {
	ProjectID string `json:"project_id,omitzero"`

	// Zero means "no paging requested" — the first page at the server default.
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`
}

type ListDefinitionsResponse struct {
	// Page describes the window returned, so a caller can say "1–50 of 340".
	Page        *PageInfo                     `json:"page,omitempty"`
	Definitions []*entities.ProcessDefinition `json:"definitions,omitzero"`
	Err         error                         `json:"err,omitzero"`
}

// PageInfo describes the window returned.
type PageInfo struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}

func (r ListDefinitionsResponse) Failed() error { return r.Err }

type GetDefinitionRequest struct {
	ID string `json:"id"`
}

type GetDefinitionResponse struct {
	Definition *entities.ProcessDefinition `json:"definition,omitzero"`
	Err        error                       `json:"err,omitzero"`
}

func (r GetDefinitionResponse) Failed() error { return r.Err }

type CreateDefinitionRequest struct {
	Definition *entities.ProcessDefinition `json:"definition,omitzero"`
}

type CreateDefinitionResponse struct {
	ID  uuid.UUID `json:"id"`
	Err error     `json:"err,omitzero"`
}

func (r CreateDefinitionResponse) Failed() error { return r.Err }

type DeleteDefinitionRequest struct {
	ID string `json:"id"`
}

type DeleteDefinitionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteDefinitionResponse) Failed() error { return r.Err }

type ExportDefinitionRequest struct {
	ID string `json:"id"`
}

type ExportDefinitionResponse struct {
	XML []byte `json:"xml,omitzero"`
	Err error  `json:"err,omitzero"`
}

func (r ExportDefinitionResponse) Failed() error { return r.Err }

// ListJavaScriptConditionsRequest asks for the javascript-conditions worklist.
// It carries nothing: the scope is the caller's tenant, resolved from context.
type ListJavaScriptConditionsRequest struct{}

// ListJavaScriptConditionsResponse is the worklist. Usages is always present —
// an empty list is the answer an operator is working toward, and `[]` says that
// where an omitted field would leave them guessing.
type ListJavaScriptConditionsResponse struct {
	Usages []entities.JavaScriptConditionUsage `json:"usages"`
	Err    error                               `json:"err,omitzero"`
}

func (r ListJavaScriptConditionsResponse) Failed() error { return r.Err }

type ImportDefinitionRequest struct {
	ProjectID string `json:"project_id"`
	XML       []byte `json:"xml"`
}

type ImportDefinitionResponse struct {
	ID  uuid.UUID `json:"id"`
	Err error     `json:"err,omitzero"`
}

func (r ImportDefinitionResponse) Failed() error { return r.Err }
