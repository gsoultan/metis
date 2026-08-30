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

type GetOrganizationRequest struct {
	ID string `json:"id"`
}

type GetOrganizationResponse struct {
	Organization entities.Organization `json:"organization"`
	Err          error                 `json:"err,omitzero"`
}

type ListOrganizationsRequest struct{}

type ListOrganizationsResponse struct {
	Organizations []entities.Organization `json:"organizations"`
	Err           error                   `json:"err,omitzero"`
}

type UpdateOrganizationRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateOrganizationResponse struct {
	Err error `json:"err,omitzero"`
}

type DeleteOrganizationRequest struct {
	ID string `json:"id"`
}

type DeleteOrganizationResponse struct {
	Err error `json:"err,omitzero"`
}
