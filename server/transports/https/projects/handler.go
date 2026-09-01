package projects

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/project"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps project.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/projects", httptransport.NewServer(
		eps.CreateProject,
		decodeCreateProjectRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/projects", httptransport.NewServer(
		eps.ListProjects,
		decodeListProjectsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/projects/{id}", httptransport.NewServer(
		eps.GetProject,
		decodeGetProjectRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/projects/{id}", httptransport.NewServer(
		eps.UpdateProject,
		decodeUpdateProjectRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/projects/{id}", httptransport.NewServer(
		eps.DeleteProject,
		decodeDeleteProjectRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeCreateProjectRequest(_ context.Context, r *http.Request) (any, error) {
	var req project.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeListProjectsRequest(_ context.Context, _ *http.Request) (any, error) {
	return project.ListProjectsRequest{}, nil
}

func decodeGetProjectRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return project.GetProjectRequest{ID: id}, nil
}

func decodeUpdateProjectRequest(_ context.Context, r *http.Request) (any, error) {
	var req project.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeDeleteProjectRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return project.DeleteProjectRequest{ID: id}, nil
}
