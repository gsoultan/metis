package projects

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	pbentities "github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/server/endpoints/project"
	"github.com/gsoultan/metis/server/transports/adapters"
	"github.com/gsoultan/metis/server/transports/grpcs/common"
)

type ProjectHandler struct {
	eps project.Endpoints
}

func NewHandler(eps project.Endpoints) *ProjectHandler {
	return &ProjectHandler{eps: eps}
}

func (h *ProjectHandler) CreateProject(ctx context.Context, req *connect.Request[pbendpoints.CreateProjectRequest]) (*connect.Response[pbendpoints.CreateProjectResponse], error) {
	response, err := h.eps.CreateProject(ctx, project.CreateProjectRequest{
		OrganizationID: req.Msg.OrganizationId,
		Name:           req.Msg.Name,
		Description:    req.Msg.Description,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(project.CreateProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.CreateProjectResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.CreateProjectResponse{
		Project: adapters.ProjectPBAdapter{Project: resp.Project}.ToProto(),
		Error:   common.ErrString(resp.Err),
	}), nil
}

func (h *ProjectHandler) GetProject(ctx context.Context, req *connect.Request[pbendpoints.GetProjectRequest]) (*connect.Response[pbendpoints.GetProjectResponse], error) {
	response, err := h.eps.GetProject(ctx, project.GetProjectRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(project.GetProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.GetProjectResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.GetProjectResponse{
		Project: adapters.ProjectPBAdapter{Project: resp.Project}.ToProto(),
		Error:   common.ErrString(resp.Err),
	}), nil
}

func (h *ProjectHandler) ListProjects(ctx context.Context, req *connect.Request[pbendpoints.ListProjectsRequest]) (*connect.Response[pbendpoints.ListProjectsResponse], error) {
	response, err := h.eps.ListProjects(ctx, project.ListProjectsRequest{
		OrganizationID: req.Msg.OrganizationId,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(project.ListProjectsResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.ListProjectsResponse, got %T", response)
	}
	pbProjects := make([]*pbentities.Project, len(resp.Projects))
	for i, p := range resp.Projects {
		pbProjects[i] = adapters.ProjectPBAdapter{Project: p}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListProjectsResponse{
		Projects: pbProjects,
		Error:    common.ErrString(resp.Err),
	}), nil
}

func (h *ProjectHandler) UpdateProject(ctx context.Context, req *connect.Request[pbendpoints.UpdateProjectRequest]) (*connect.Response[pbendpoints.UpdateProjectResponse], error) {
	response, err := h.eps.UpdateProject(ctx, project.UpdateProjectRequest{
		ID:             req.Msg.Id,
		OrganizationID: req.Msg.OrganizationId,
		Name:           req.Msg.Name,
		Description:    req.Msg.Description,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(project.UpdateProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.UpdateProjectResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.UpdateProjectResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *ProjectHandler) DeleteProject(ctx context.Context, req *connect.Request[pbendpoints.DeleteProjectRequest]) (*connect.Response[pbendpoints.DeleteProjectResponse], error) {
	response, err := h.eps.DeleteProject(ctx, project.DeleteProjectRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(project.DeleteProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.DeleteProjectResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.DeleteProjectResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}
