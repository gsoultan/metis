// Package approval exposes approval lines: approvers defined as data, the
// requests submitted on them and the decisions made.
package approval

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints/principal"
)

// Endpoints are the approval line operations.
type Endpoints struct {
	DefineApprovalLine   endpoint.Endpoint
	GetApprovalLine      endpoint.Endpoint
	SubmitApproval       endpoint.Endpoint
	GetApprovalRequest   endpoint.Endpoint
	ListApprovalRequests endpoint.Endpoint
	DecideApproval       endpoint.Endpoint
	ListApprovalsBehind  endpoint.Endpoint
	MoveApprovals        endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		DefineApprovalLine:   makeDefine(s),
		GetApprovalLine:      makeGetLine(s),
		SubmitApproval:       makeSubmit(s),
		GetApprovalRequest:   makeGetRequest(s),
		ListApprovalRequests: makeListRequests(s),
		DecideApproval:       makeDecide(s),
		ListApprovalsBehind:  makeBehind(s),
		MoveApprovals:        makeMove(s),
	}
}

func makeDefine(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DefineLineRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a DefineLineRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return LineResponse{Err: err}, nil
		}
		line, err := s.DefineApprovalLine(ctx, projectID, req.Line)
		if err != nil {
			return LineResponse{Err: err}, nil
		}
		return LineResponse{Line: &line}, nil
	}
}

func makeGetLine(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetLineRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a GetLineRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return LineResponse{Err: err}, nil
		}
		line, err := s.GetApprovalLine(ctx, projectID, req.Key)
		if err != nil {
			return LineResponse{Err: err}, nil
		}
		return LineResponse{Line: &line}, nil
	}
}

func makeSubmit(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SubmitRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a SubmitRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		// Who submitted is who is signed in, never what the body says: the
		// request goes back to them to revise.
		actor, err := principal.Username(ctx)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		got, err := s.SubmitApproval(ctx, projectID, req.Key, req.Variables, actor)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		return RequestResponse{Request: &got}, nil
	}
}

func makeGetRequest(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetRequestRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a GetRequestRequest, got %T", request)
		}
		id, err := parseID("request", req.InstanceID)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		got, err := s.GetApprovalRequest(ctx, id)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		return RequestResponse{Request: &got}, nil
	}
}

func makeListRequests(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListRequestsRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a ListRequestsRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return ListRequestsResponse{Err: err}, nil
		}
		got, err := s.ListApprovalRequests(ctx, projectID, req.Key, req.SubmittedBy)
		if err != nil {
			return ListRequestsResponse{Err: err}, nil
		}
		if got == nil {
			got = []entities.ApprovalRequest{}
		}
		return ListRequestsResponse{Requests: got}, nil
	}
}

func makeDecide(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DecideRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a DecideRequest, got %T", request)
		}
		id, err := parseID("task", req.TaskID)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		actor, err := principal.Username(ctx)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		got, err := s.DecideApproval(ctx, id, req.Decision, req.Comment, req.Variables, actor)
		if err != nil {
			return RequestResponse{Err: err}, nil
		}
		return RequestResponse{Request: &got}, nil
	}
}

func makeBehind(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(BehindRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a BehindRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return BehindResponse{Err: err}, nil
		}
		got, err := s.ListApprovalsBehind(ctx, projectID, req.Key)
		if err != nil {
			return BehindResponse{Err: err}, nil
		}
		return BehindResponse{Takeover: &got}, nil
	}
}

func makeMove(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(MoveRequest)
		if !ok {
			return nil, fmt.Errorf("approval: expected a MoveRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return MoveResponse{Err: err}, nil
		}
		ids := make([]uuid.UUID, 0, len(req.Requests))
		for _, raw := range req.Requests {
			id, err := parseID("request", raw)
			if err != nil {
				return MoveResponse{Err: err}, nil
			}
			ids = append(ids, id)
		}
		// The move is recorded as whoever is signed in, as a migration is.
		actor, err := principal.Username(ctx)
		if err != nil {
			return MoveResponse{Err: err}, nil
		}
		got, err := s.MoveApprovals(ctx, projectID, req.Key, ids, actor)
		if err != nil {
			return MoveResponse{Err: err}, nil
		}
		return MoveResponse{Result: &got}, nil
	}
}

func parseID(what, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%s id %q is not a valid identifier", what, raw)
	}
	return id, nil
}
