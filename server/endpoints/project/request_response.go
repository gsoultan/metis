package project

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

type CreateProjectRequest struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
}

type CreateProjectResponse struct {
	Project entities.Project `json:"project"`
	Err     error            `json:"err,omitzero"`
}

func (r CreateProjectResponse) Failed() error { return r.Err }

type GetProjectRequest struct {
	ID string `json:"id"`
}

type GetProjectResponse struct {
	Project entities.Project `json:"project"`
	Err     error            `json:"err,omitzero"`
}

func (r GetProjectResponse) Failed() error { return r.Err }

type ListProjectsRequest struct {
	OrganizationID string `json:"organization_id"`
}

type ListProjectsResponse struct {
	Projects []entities.Project `json:"projects"`
	Err      error              `json:"err,omitzero"`
}

func (r ListProjectsResponse) Failed() error { return r.Err }

type UpdateProjectRequest struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
}

type UpdateProjectResponse struct {
	Err error `json:"err,omitzero"`
}

func (r UpdateProjectResponse) Failed() error { return r.Err }

type DeleteProjectRequest struct {
	ID string `json:"id"`
}

type DeleteProjectResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteProjectResponse) Failed() error { return r.Err }
