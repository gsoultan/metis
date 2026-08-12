package definition

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type ListDefinitionsRequest struct {
	ProjectID string `json:"project_id,omitzero"`
}

type ListDefinitionsResponse struct {
	Definitions []*entities.ProcessDefinition `json:"definitions,omitzero"`
	Err         error                        `json:"err,omitzero"`
}

func (r ListDefinitionsResponse) Failed() error { return r.Err }

type GetDefinitionRequest struct {
	ID string `json:"id"`
}

type GetDefinitionResponse struct {
	Definition *entities.ProcessDefinition `json:"definition,omitzero"`
	Err        error                      `json:"err,omitzero"`
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

type ImportDefinitionRequest struct {
	XML []byte `json:"xml"`
}

type ImportDefinitionResponse struct {
	ID  uuid.UUID `json:"id"`
	Err error     `json:"err,omitzero"`
}

func (r ImportDefinitionResponse) Failed() error { return r.Err }
