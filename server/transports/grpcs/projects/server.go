package projects

import (
	"context"
	"fmt"

	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/api/proto/services"
	"github.com/gsoultan/metis/server/endpoints/project"
	"github.com/gsoultan/metis/server/transports/adapters"
	"github.com/gsoultan/metis/server/transports/grpcs/common"
)

type Server struct {
	services.UnimplementedProjectServiceServer
	createProject grpctransport.Handler
	getProject    grpctransport.Handler
	listProjects  grpctransport.Handler
	updateProject grpctransport.Handler
	deleteProject grpctransport.Handler
}

func NewServer(eps project.Endpoints) *Server {
	return &Server{
		createProject: grpctransport.NewServer(
			eps.CreateProject,
			decodeGRPCCreateProjectRequest,
			encodeGRPCCreateProjectResponse,
		),
		getProject: grpctransport.NewServer(
			eps.GetProject,
			decodeGRPCGetProjectRequest,
			encodeGRPCGetProjectResponse,
		),
		listProjects: grpctransport.NewServer(
			eps.ListProjects,
			decodeGRPCListProjectsRequest,
			encodeGRPCListProjectsResponse,
		),
		updateProject: grpctransport.NewServer(
			eps.UpdateProject,
			decodeGRPCUpdateProjectRequest,
			encodeGRPCUpdateProjectResponse,
		),
		deleteProject: grpctransport.NewServer(
			eps.DeleteProject,
			decodeGRPCDeleteProjectRequest,
			encodeGRPCDeleteProjectResponse,
		),
	}
}

func (s *Server) CreateProject(ctx context.Context, req *endpoints.CreateProjectRequest) (*endpoints.CreateProjectResponse, error) {
	_, resp, err := s.createProject.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.CreateProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.CreateProjectResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) GetProject(ctx context.Context, req *endpoints.GetProjectRequest) (*endpoints.GetProjectResponse, error) {
	_, resp, err := s.getProject.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.GetProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.GetProjectResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ListProjects(ctx context.Context, req *endpoints.ListProjectsRequest) (*endpoints.ListProjectsResponse, error) {
	_, resp, err := s.listProjects.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ListProjectsResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.ListProjectsResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) UpdateProject(ctx context.Context, req *endpoints.UpdateProjectRequest) (*endpoints.UpdateProjectResponse, error) {
	_, resp, err := s.updateProject.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.UpdateProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.UpdateProjectResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) DeleteProject(ctx context.Context, req *endpoints.DeleteProjectRequest) (*endpoints.DeleteProjectResponse, error) {
	_, resp, err := s.deleteProject.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.DeleteProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.DeleteProjectResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCCreateProjectRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.CreateProjectRequest)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.CreateProjectRequest, got %T", grpcReq)
	}
	return project.CreateProjectRequest{
		OrganizationID: req.OrganizationId,
		Name:           req.Name,
		Description:    req.Description,
	}, nil
}

func encodeGRPCCreateProjectResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(project.CreateProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.CreateProjectResponse, got %T", response)
	}
	return &endpoints.CreateProjectResponse{
		Project: adapters.ProjectPBAdapter{Project: resp.Project}.ToProto(),
		Error:   common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCGetProjectRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.GetProjectRequest)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.GetProjectRequest, got %T", grpcReq)
	}
	return project.GetProjectRequest{ID: req.Id}, nil
}

func encodeGRPCGetProjectResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(project.GetProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.GetProjectResponse, got %T", response)
	}
	return &endpoints.GetProjectResponse{
		Project: adapters.ProjectPBAdapter{Project: resp.Project}.ToProto(),
		Error:   common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCListProjectsRequest(_ context.Context, _ any) (any, error) {
	return project.ListProjectsRequest{}, nil
}

func encodeGRPCListProjectsResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(project.ListProjectsResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.ListProjectsResponse, got %T", response)
	}
	var projects []*entities.Project
	if len(resp.Projects) > 0 {
		projects = make([]*entities.Project, 0, len(resp.Projects))
		for _, p := range resp.Projects {
			projects = append(projects, adapters.ProjectPBAdapter{Project: p}.ToProto())
		}
	}
	return &endpoints.ListProjectsResponse{Projects: projects, Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCUpdateProjectRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.UpdateProjectRequest)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.UpdateProjectRequest, got %T", grpcReq)
	}
	return project.UpdateProjectRequest{
		ID:             req.Id,
		OrganizationID: req.OrganizationId,
		Name:           req.Name,
		Description:    req.Description,
	}, nil
}

func encodeGRPCUpdateProjectResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(project.UpdateProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.UpdateProjectResponse, got %T", response)
	}
	return &endpoints.UpdateProjectResponse{Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCDeleteProjectRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.DeleteProjectRequest)
	if !ok {
		return nil, fmt.Errorf("projects: expected a *endpoints.DeleteProjectRequest, got %T", grpcReq)
	}
	return project.DeleteProjectRequest{ID: req.Id}, nil
}

func encodeGRPCDeleteProjectResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(project.DeleteProjectResponse)
	if !ok {
		return nil, fmt.Errorf("projects: expected a project.DeleteProjectResponse, got %T", response)
	}
	return &endpoints.DeleteProjectResponse{Error: common.ErrString(resp.Err)}, nil
}
