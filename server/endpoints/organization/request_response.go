package organization

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

type CreateOrganizationRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreateOrganizationResponse struct {
	Organization entities.Organization `json:"organization"`
	Err          error                 `json:"err,omitzero"`
}

func (r CreateOrganizationResponse) Failed() error { return r.Err }

type GetOrganizationRequest struct {
	ID string `json:"id"`
}

type GetOrganizationResponse struct {
	Organization entities.Organization `json:"organization"`
	Err          error                 `json:"err,omitzero"`
}

func (r GetOrganizationResponse) Failed() error { return r.Err }

type ListOrganizationsRequest struct{}

type ListOrganizationsResponse struct {
	Organizations []entities.Organization `json:"organizations"`
	Err           error                   `json:"err,omitzero"`
}

func (r ListOrganizationsResponse) Failed() error { return r.Err }

type UpdateOrganizationRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateOrganizationResponse struct {
	Err error `json:"err,omitzero"`
}

func (r UpdateOrganizationResponse) Failed() error { return r.Err }

type DeleteOrganizationRequest struct {
	ID string `json:"id"`
}

type DeleteOrganizationResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteOrganizationResponse) Failed() error { return r.Err }
